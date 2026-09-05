// Package audio captures the microphone at exactly what whisper.cpp wants:
// 16 kHz, mono, float32. No resampling happens downstream.
package audio

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"unsafe"

	"github.com/gen2brain/malgo"
)

const (
	SampleRate = 16_000
	channels   = 1
)

// Device is a selectable microphone.
type Device struct {
	Name      string
	IsDefault bool
	id        malgo.DeviceID
}

// Recorder records to memory until stopped. One recording at a time.
//
// The capture device is opened once and kept, stopped, between recordings;
// Start and Stop only start and stop its IO. Rebuilding it per recording
// (the previous design) was not just slow — it broke the sound cues. On
// macOS the capture AudioUnit that miniaudio creates attaches to the
// default *output* device first, creates an IO proc on it, then tears that
// down and tells coreaudiod the speaker session is "Stopped" before it is
// pointed at the microphone. That happened underneath the sound Player's
// live output stream on the very same device, on every key-down: cues came
// out stuttering or not at all, while the Player's own callback kept
// running on time and saw nothing wrong. Opening the unit once — before the
// Player opens its stream (see daemon.New) — takes that out of the per-
// dictation path entirely. A stopped unit does not run IO, so the mic
// indicator behaves exactly as before: on while recording, off otherwise.
type Recorder struct {
	deviceName string
	maxSamples int

	mu       sync.Mutex
	ctx      *malgo.AllocatedContext
	dev      *malgo.Device
	running  bool // IO started on dev
	buf      []float32
	level    float64
	overflow bool
}

// NewRecorder targets the named device ("" = system default) and refuses to
// keep more than maxSeconds, so a forgotten toggle cannot record forever.
func NewRecorder(deviceName string, maxSeconds int) *Recorder {
	if maxSeconds <= 0 {
		maxSeconds = 300
	}
	return &Recorder{deviceName: deviceName, maxSamples: maxSeconds * SampleRate}
}

func newContext() (*malgo.AllocatedContext, error) {
	return malgo.InitContext(nil, malgo.ContextConfig{}, nil)
}

// ListDevices enumerates capture devices.
func ListDevices() ([]Device, error) {
	ctx, err := newContext()
	if err != nil {
		return nil, err
	}
	defer func() { _ = ctx.Uninit(); ctx.Free() }()
	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(infos))
	for _, in := range infos {
		out = append(out, Device{Name: in.Name(), IsDefault: in.IsDefault != 0, id: in.ID})
	}
	return out, nil
}

// DefaultDeviceName returns the system default microphone's name, or "".
func DefaultDeviceName() string {
	devs, err := ListDevices()
	if err != nil {
		return ""
	}
	for _, d := range devs {
		if d.IsDefault {
			return d.Name
		}
	}
	if len(devs) > 0 {
		return devs[0].Name
	}
	return ""
}

// Recording reports whether the microphone is currently capturing.
func (r *Recorder) Recording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Level is the most recent smoothed RMS, 0–1, for a live meter.
func (r *Recorder) Level() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.level
}

// Duration is how much audio has been captured so far.
func (r *Recorder) Duration() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return float64(len(r.buf)) / SampleRate
}

// Overflowed reports whether the max duration was hit.
func (r *Recorder) Overflowed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.overflow
}

func (r *Recorder) onData(_ []byte, in []byte, frames uint32) {
	if frames == 0 || len(in) == 0 {
		return
	}
	samples := unsafe.Slice((*float32)(unsafe.Pointer(&in[0])), int(frames)*channels)

	var sum float64
	for _, s := range samples {
		sum += float64(s) * float64(s)
	}
	rms := math.Sqrt(sum / float64(len(samples)))

	r.mu.Lock()
	if len(r.buf)+len(samples) > r.maxSamples {
		r.overflow = true
	} else {
		r.buf = append(r.buf, samples...)
	}
	// Smooth the meter a little so the pill doesn't strobe.
	r.level = 0.5*r.level + 0.5*math.Min(rms*12, 1)
	r.mu.Unlock()
}

// Prepare opens the capture device without starting it, so the first
// key-down pays only for Start, and so the one-time speaker-side detour
// described on Recorder happens now, before any output stream exists.
// Errors are not fatal: Start will simply try again.
func (r *Recorder) Prepare() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.openLocked()
}

// openLocked initialises ctx/dev if they are not already. Caller holds r.mu.
func (r *Recorder) openLocked() error {
	if r.dev != nil {
		return nil
	}
	ctx, err := newContext()
	if err != nil {
		return fmt.Errorf("audio backend: %w", err)
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = channels
	cfg.SampleRate = SampleRate
	cfg.PeriodSizeInMilliseconds = 40
	cfg.Alsa.NoMMap = 1

	if r.deviceName != "" {
		infos, err := ctx.Devices(malgo.Capture)
		if err == nil {
			for i := range infos {
				if infos[i].Name() == r.deviceName {
					cfg.Capture.DeviceID = infos[i].ID.Pointer()
					break
				}
			}
		}
	}

	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: r.onData})
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return fmt.Errorf("open microphone: %w", err)
	}
	r.ctx, r.dev = ctx, dev
	return nil
}

// closeLocked releases ctx/dev. Caller holds r.mu.
func (r *Recorder) closeLocked() {
	if r.dev != nil {
		if r.running {
			_ = r.dev.Stop()
		}
		r.dev.Uninit()
	}
	if r.ctx != nil {
		_ = r.ctx.Uninit()
		r.ctx.Free()
	}
	r.dev, r.ctx, r.running = nil, nil, false
}

// Start begins capturing. The device is opened on first use (or if a
// previous one was lost) and then reused.
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	if err := r.openLocked(); err != nil {
		return err
	}
	r.buf = r.buf[:0]
	r.level = 0
	r.overflow = false
	if err := r.dev.Start(); err != nil {
		// The device we were holding may be gone (unplugged, or the system
		// default changed underneath a named device). Rebuild once.
		r.closeLocked()
		if err2 := r.openLocked(); err2 != nil {
			return fmt.Errorf("start microphone: %w", err)
		}
		if err2 := r.dev.Start(); err2 != nil {
			r.closeLocked()
			return fmt.Errorf("start microphone: %w", err2)
		}
	}
	r.running = true
	return nil
}

// Stop ends capturing and returns everything captured. The device stays
// open, stopped, for the next Start.
func (r *Recorder) Stop() []float32 {
	r.mu.Lock()
	dev, running := r.dev, r.running
	r.running = false
	r.mu.Unlock()

	if dev != nil && running {
		_ = dev.Stop()
	}

	r.mu.Lock()
	out := make([]float32, len(r.buf))
	copy(out, r.buf)
	r.buf = r.buf[:0]
	r.level = 0
	r.mu.Unlock()
	return out
}

// Close stops capturing if needed and releases the device for good.
func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
}

// Cancel discards the recording.
func (r *Recorder) Cancel() { _ = r.Stop() }

// ErrNoDevice is returned when no microphone is available.
var ErrNoDevice = errors.New("no microphone found")
