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
type Recorder struct {
	deviceName string
	maxSamples int

	mu       sync.Mutex
	ctx      *malgo.AllocatedContext
	dev      *malgo.Device
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

// Recording reports whether a stream is open.
func (r *Recorder) Recording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dev != nil
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
	r.level = 0.6*r.level + 0.4*math.Min(rms*4, 1)
	r.mu.Unlock()
}

// Start opens the microphone.
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.buf = r.buf[:0]
	r.level = 0
	r.overflow = false
	if err := dev.Start(); err != nil {
		dev.Uninit()
		_ = ctx.Uninit()
		ctx.Free()
		return fmt.Errorf("start microphone: %w", err)
	}
	r.ctx, r.dev = ctx, dev
	return nil
}

// Stop closes the microphone and returns everything captured.
func (r *Recorder) Stop() []float32 {
	r.mu.Lock()
	dev, ctx := r.dev, r.ctx
	r.dev, r.ctx = nil, nil
	r.mu.Unlock()

	if dev != nil {
		_ = dev.Stop()
		dev.Uninit()
	}
	if ctx != nil {
		_ = ctx.Uninit()
		ctx.Free()
	}

	r.mu.Lock()
	out := make([]float32, len(r.buf))
	copy(out, r.buf)
	r.buf = r.buf[:0]
	r.level = 0
	r.mu.Unlock()
	return out
}

// Cancel discards the recording.
func (r *Recorder) Cancel() { _ = r.Stop() }

// ErrNoDevice is returned when no microphone is available.
var ErrNoDevice = errors.New("no microphone found")
