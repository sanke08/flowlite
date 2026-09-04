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
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return p, err
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = 1
	cfg.SampleRate = Rate
	cfg.PeriodSizeInMilliseconds = 10
	cfg.Alsa.NoMMap = 1

	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: p.mix})
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return p, err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		_ = ctx.Uninit()
		ctx.Free()
		return p, err
	}
	p.ctx, p.dev = ctx, dev
	return p, nil
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
		t := time.NewTicker(380 * time.Millisecond)
		defer t.Stop()
		// First tick lands just after the Stop cue finishes, not on top of it.
		select {
		case <-time.After(260 * time.Millisecond):
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
