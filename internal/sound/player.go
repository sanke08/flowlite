package sound

import (
	"sync"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"
)

// Player mixes cues into one persistent output stream. Keeping the device
// open means the Start cue plays the instant the key goes down instead of
// after a device-open delay, which is the difference between "responsive"
// and "did that register?".
type Player struct {
	mu      sync.Mutex
	ctx     *malgo.AllocatedContext
	dev     *malgo.Device
	active  []voice
	enabled bool

	workMu   sync.Mutex
	workStop chan struct{}

	// lifecycleMu serialises Reopen and Close against each other and against
	// themselves: a wake notification racing shutdown, or two wake
	// notifications racing each other, would otherwise both open (or free) a
	// device and step on whichever field the other just wrote — leaking one
	// stream or double-freeing another.
	lifecycleMu sync.Mutex
}

type voice struct {
	buf []float32
	pos int
}

// NewPlayer opens the default output. Errors are non-fatal for the app:
// a nil-safe Player just stays silent.
func NewPlayer(enabled bool) (*Player, error) {
	p := &Player{enabled: enabled}
	if !enabled {
		return p, nil
	}
	ctx, dev, err := openWithRetry(p.mix)
	if err != nil {
		return p, err
	}
	p.ctx, p.dev = ctx, dev
	return p, nil
}

// openRetries and openRetryDelay: applying a settings change or an update
// reloads FlowLite by replacing its own process image in place (same pid,
// see internal/cli/proc_unix.go reexecSelf) right after the old process just
// tore down this exact device. CoreAudio can take a moment to finish
// releasing that pid's previous registration before it will hand out a new
// one, and unlike a cold start there is no separate dying process here to
// wait out — so a first failure here is retried rather than taken as final.
// Nothing times this out from the caller's side — reload(pid) just sends the
// signal and returns, it never waits for the reopen to finish — so there is
// no reason to be stingy here; a cold start never needs more than the first
// attempt, so a generous budget only ever spends time on the case it exists
// for.
const (
	openRetries    = 10
	openRetryDelay = 200 * time.Millisecond
)

// openWithRetry wraps openStream with that retry.
func openWithRetry(onData malgo.DataProc) (*malgo.AllocatedContext, *malgo.Device, error) {
	var lastErr error
	for attempt := 0; attempt < openRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(openRetryDelay)
		}
		ctx, dev, err := openStream(onData)
		if err == nil {
			return ctx, dev, nil
		}
		lastErr = err
	}
	return nil, nil, lastErr
}

// openStream opens the default playback device with the settings the Player
// needs. Split out so NewPlayer and Reopen can both retry it without
// duplicating the config.
func openStream(onData malgo.DataProc) (*malgo.AllocatedContext, *malgo.Device, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, nil, err
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = 1
	cfg.SampleRate = Rate
	// 10ms left the callback too little slack: whisper.cpp can spin up to 8
	// CPU threads during transcription (see whisper.DefaultThreads), and
	// under that load the audio thread can miss its deadline and audibly
	// underrun — heard as stuttering right when Working is ticking. 25ms
	// trades a little latency for headroom against that contention.
	cfg.PeriodSizeInMilliseconds = 25
	cfg.Alsa.NoMMap = 1

	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return nil, nil, err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		_ = ctx.Uninit()
		ctx.Free()
		return nil, nil, err
	}
	return ctx, dev, nil
}

// Reopen rebuilds the output stream from scratch. Call it after the system
// wakes from sleep: closing the laptop lid suspends the CoreAudio device the
// stream was bound to, and macOS does not hand it back in a working state on
// its own — playback stays silent, or stutters, until the stream is torn
// down and reopened.
func (p *Player) Reopen() {
	if p == nil || !p.enabled {
		return
	}
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	p.mu.Lock()
	oldDev, oldCtx := p.dev, p.ctx
	p.dev, p.ctx = nil, nil
	p.active = nil
	p.mu.Unlock()

	if oldDev != nil {
		_ = oldDev.Stop()
		oldDev.Uninit()
	}
	if oldCtx != nil {
		_ = oldCtx.Uninit()
		oldCtx.Free()
	}

	ctx, dev, err := openWithRetry(p.mix)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.ctx, p.dev = ctx, dev
	p.mu.Unlock()
}

func (p *Player) mix(out []byte, _ []byte, frames uint32) {
	if frames == 0 || len(out) == 0 {
		return
	}
	dst := unsafe.Slice((*float32)(unsafe.Pointer(&out[0])), int(frames))
	for i := range dst {
		dst[i] = 0
	}
	p.mu.Lock()
	live := p.active[:0]
	for _, v := range p.active {
		n := copyAdd(dst, v.buf[v.pos:])
		v.pos += n
		if v.pos < len(v.buf) {
			live = append(live, v)
		}
	}
	p.active = live
	p.mu.Unlock()
}

func copyAdd(dst, src []float32) int {
	n := len(dst)
	if len(src) < n {
		n = len(src)
	}
	for i := 0; i < n; i++ {
		dst[i] += src[i]
	}
	return n
}

// Play queues a cue. Safe on a nil or disabled Player.
func (p *Player) Play(c Cue) {
	if p == nil || !p.enabled || p.dev == nil {
		return
	}
	p.mu.Lock()
	p.active = append(p.active, voice{buf: Samples(c)})
	p.mu.Unlock()
}

// StartWorking begins the periodic "still transcribing" tick.
func (p *Player) StartWorking() {
	if p == nil || !p.enabled || p.dev == nil {
		return
	}
	p.workMu.Lock()
	defer p.workMu.Unlock()
	if p.workStop != nil {
		return
	}
	stop := make(chan struct{})
	p.workStop = stop
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		// First tick lands just after the Stop cue finishes, not on top of it.
		select {
		case <-time.After(50 * time.Millisecond):
		case <-stop:
			return
		}
		for {
			p.Play(Working)
			select {
			case <-t.C:
			case <-stop:
				return
			}
		}
	}()
}

// StopWorking ends the tick.
func (p *Player) StopWorking() {
	if p == nil {
		return
	}
	p.workMu.Lock()
	if p.workStop != nil {
		close(p.workStop)
		p.workStop = nil
	}
	p.workMu.Unlock()
}

// Close releases the device.
func (p *Player) Close() {
	if p == nil {
		return
	}
	p.StopWorking()
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	p.mu.Lock()
	dev, ctx := p.dev, p.ctx
	p.dev, p.ctx = nil, nil
	p.mu.Unlock()
	if dev != nil {
		_ = dev.Stop()
		dev.Uninit()
	}
	if ctx != nil {
		_ = ctx.Uninit()
		ctx.Free()
	}
}
