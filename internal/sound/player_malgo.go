//go:build !darwin

// miniaudio-backed Player for Windows and Linux. On macOS see player_darwin.go:
// the AUHAL stream miniaudio opens there audibly stutters on macOS 26 even
// when this callback is provably on time and non-silent (verified with the
// per-cue liveness log and by rendering the identical samples to WAV, which
// afplay played cleanly), so macOS uses the system-managed AVAudioPlayer.
package sound

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"
)

// Player mixes cues into one persistent output stream. Keeping the device
// open means the Start cue plays the instant the key goes down instead of
// after a device-open delay, which is the difference between "responsive"
// and "did that register?".
type Player struct {
	mu     sync.Mutex
	ctx    *malgo.AllocatedContext
	active []voice

	// dev is the single source of truth for whether the output device is
	// live. It is an atomic.Pointer rather than a plain *malgo.Device so
	// Play's common-case check ("is there a device to queue onto at all?")
	// never needs p.mu just to make that decision — only mix (the real-time
	// CoreAudio callback) and Play's own append need p.mu, to keep p.active
	// consistent between them. Reopen/Close still store through this under
	// p.mu as well, so that the store and the p.active reset it pairs with
	// stay atomic with respect to Play's append.
	dev     atomic.Pointer[malgo.Device]
	enabled bool

	// Stall diagnostics, written only from mix (real-time thread, atomics
	// only, never logs) and read/reset from Play (ordinary goroutine) which
	// reports them through logf. lastCB is the previous callback's UnixNano;
	// late counts callbacks that arrived more than lateAfter since the
	// previous one, maxGapNS the worst such gap; misses counts periods
	// emitted as silence because TryLock failed.
	lastCB   atomic.Int64
	scratch  []float32    // mono mix buffer, reused across callbacks (RT thread only)
	calls    atomic.Int64 // total callbacks since open; liveness evidence for the log
	peakMil  atomic.Int64 // loudest |sample| handed to CoreAudio since last report, in 1/1000
	late     atomic.Int64
	maxGapNS atomic.Int64
	misses   atomic.Int64
	logf     func(format string, args ...any)

	watchOnce sync.Once
	watchStop chan struct{}

	workMu   sync.Mutex
	workStop chan struct{}

	// lifecycleMu serialises Reopen and Close against each other and against
	// themselves: a wake notification racing shutdown, or two wake
	// notifications racing each other, would otherwise both open (or free) a
	// device and step on whichever field the other just wrote — leaking one
	// stream or double-freeing another.
	lifecycleMu sync.Mutex

	// closed is set by Close (under lifecycleMu) and never cleared. Reopen
	// checks it under the same lock so a wake notification or the watchdog
	// that lost the race with Close cannot open a fresh context+device onto a
	// Player nobody will ever Close again — which would leak one stream per
	// reload and keep playing cues after shutdown. watch also polls it so the
	// goroutine exits even if it is mid-iteration when Close runs.
	closed atomic.Bool
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
	// Render every cue now, before the device exists. Samples() caches
	// lazily, so without this the first Play of each cue after a (re)start
	// synthesised it on demand — 4-5ms of math for Start/Stop/Done — and did
	// so while holding p.mu, the very lock mix needs on CoreAudio's
	// real-time thread. Under any CPU pressure that was enough to blow the
	// 25ms IO budget right as the first Start cue was due: coreaudiod logs
	// it as "skipping cycle due to overload" and the user hears a clipped,
	// stuttering or missing first cue after every `make dev`.
	for c := Start; c <= Error; c++ {
		Samples(c)
	}
	ctx, dev, err := openWithRetry(p.mix)
	if err != nil {
		return p, err
	}
	p.ctx = ctx
	p.dev.Store(dev)
	p.watchOnce.Do(func() {
		p.watchStop = make(chan struct{})
		go p.watch()
	})
	return p, nil
}

// lateAfter is the callback gap that counts as a stall. The device period
// is 25ms, so anything past twice that means CoreAudio skipped a cycle.
const lateAfter = 50 * time.Millisecond

// channels is the output stream's channel count; see openStream.
const channels = 2

// deadAfter is how long the callback may go unheard before Play treats the
// stream as dead and rebuilds it (see Play). Generous next to the 25ms
// period so a mere late cycle never triggers a teardown.
const deadAfter = 400 * time.Millisecond

// SetLogger installs where stall reports go. Reports are emitted lazily from
// Play (never from the real-time callback), so a stall shows up in the log
// the next time a cue is queued, as "audio: N late callbacks (max Xms)".
func (p *Player) SetLogger(fn func(format string, args ...any)) {
	if p == nil {
		return
	}
	p.logf = fn
}

// report flushes stall counters to the logger, if any accumulated.
func (p *Player) report() {
	if p.logf == nil {
		return
	}
	late, misses := p.late.Swap(0), p.misses.Swap(0)
	maxGap := time.Duration(p.maxGapNS.Swap(0))
	calls := p.calls.Swap(0)
	peak := float64(p.peakMil.Swap(0)) / 1000
	var since time.Duration
	if last := p.lastCB.Load(); last != 0 {
		since = time.Since(time.Unix(0, last))
	}
	// One line per cue: how many callbacks ran since the previous cue and
	// how long ago the last one was. Zero callbacks, or a last one far in
	// the past, is the stream being dead — nothing else in this process can
	// tell that apart from "working, but the speaker is silent".
	p.logf("audio: %d callbacks since last cue, last %s ago, peak %.2f, %d late (max gap %s), %d dropped",
		calls, since.Round(time.Millisecond), peak, late, maxGap.Round(time.Millisecond), misses)
}

// watch is the stream watchdog: a stream that stops asking for samples
// while dev is live is rebuilt without waiting for a Play to notice. Runs
// for the Player's lifetime; Close stops it (by closing watchStop and by
// setting closed, either of which ends the loop).
func (p *Player) watch() {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-p.watchStop:
			return
		case <-t.C:
		}
		if p.closed.Load() {
			return
		}
		if p.dev.Load() == nil {
			continue
		}
		last := p.lastCB.Load()
		if last == 0 {
			continue // freshly opened; CoreAudio has not called yet
		}
		if gap := time.Since(time.Unix(0, last)); gap > deadAfter {
			if p.logf != nil {
				p.logf("audio: output stream silent for %s — reopening", gap.Round(time.Millisecond))
			}
			p.Reopen()
		}
	}
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
	// Stereo, not mono. The cues are mono and mix writes each sample to both
	// channels itself. Asking miniaudio for a 1-channel stream on the
	// built-in (2-channel) speakers made it remap channels inside the
	// real-time callback, and on macOS 26 that path audibly stutters even
	// when the callback is perfectly on time — verified by A/B listening
	// with identical buffers: mono stuttered, stereo was clean.
	cfg.Playback.Channels = channels
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
	if p.closed.Load() {
		// Close won the race (or already ran): the device it freed must
		// stay freed. Opening another one here would have no owner.
		return
	}

	p.mu.Lock()
	oldDev, oldCtx := p.dev.Swap(nil), p.ctx
	p.ctx = nil
	// Queued voices are kept: a cue queued onto a stream that died is the
	// cue the user is waiting for, and it plays the instant the new stream
	// starts. Their pos is left as is; a partially played cue resumes.
	p.lastCB.Store(0) // the gap across a teardown is not a stall
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
	p.ctx = ctx
	p.dev.Store(dev)
	p.mu.Unlock()
}

func (p *Player) mix(out []byte, _ []byte, frames uint32) {
	if frames == 0 || len(out) == 0 {
		return
	}
	now := time.Now().UnixNano()
	p.calls.Add(1)
	if prev := p.lastCB.Swap(now); prev != 0 {
		if gap := now - prev; gap > int64(lateAfter) {
			p.late.Add(1)
			for {
				cur := p.maxGapNS.Load()
				if gap <= cur || p.maxGapNS.CompareAndSwap(cur, gap) {
					break
				}
			}
		}
	}
	// dst is a mono scratch mix; it is fanned out to every channel below.
	n := int(frames)
	mono := p.scratch
	if cap(mono) < n {
		mono = make([]float32, n)
		p.scratch = mono
	}
	dst := mono[:n]
	for i := range dst {
		dst[i] = 0
	}
	defer func() {
		inter := unsafe.Slice((*float32)(unsafe.Pointer(&out[0])), n*channels)
		for i, s := range dst {
			for c := 0; c < channels; c++ {
				inter[i*channels+c] = s
			}
		}
	}()
	// This runs on CoreAudio's real-time IO thread with a hard deadline
	// (the 25ms period above). Never block here: if Play happens to hold
	// p.mu at this instant, emit this one period as silence and move on —
	// a 25ms gap in a cue is far less audible than the stall-then-burst
	// that a blocking Lock produces when the callback misses its cycle.
	if !p.mu.TryLock() {
		p.misses.Add(1)
		return
	}
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
	// Peak of what actually left for the speaker: proves the mix is not
	// silent when the log says the stream is alive but nothing is heard.
	var peak float32
	for _, v := range dst {
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	if pm := int64(peak * 1000); pm > p.peakMil.Load() {
		p.peakMil.Store(pm)
	}
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
	if p == nil || !p.enabled {
		return
	}
	// Lock-free fast-out: reading dev through the atomic is race-safe on its
	// own (unlike the plain-pointer read this replaced), so a Play() that
	// lands while the device is torn down — Reopen's brief window, or after
	// Close — returns without ever touching p.mu, exactly like the pre-fix
	// code did. Device-open is the normal steady state, though, and in that
	// case (as before this fix, and same as today) Play() still takes p.mu
	// to append safely against mix; this restores the nil-check fast path,
	// it does not add a new one to the common case. Reopen/Close only ever
	// transition dev from non-nil to nil (or back) while also holding p.mu
	// around the p.active reset that goes with it, so once we do take p.mu
	// below, dev's value at that point is exactly what matters for whether
	// this append is safe to make.
	if p.dev.Load() == nil {
		return
	}
	// Resolve the buffer before taking p.mu: Samples is a cache hit after
	// NewPlayer's pre-render, but keeping any non-trivial work outside the
	// lock is what keeps mix's TryLock from ever failing in practice.
	buf := Samples(c)
	p.report()
	// Dead-stream check. CoreAudio can stop calling a client's IO proc
	// without any error reaching miniaudio — seen when the same process
	// opens the built-in microphone and the HAL rebuilds the built-in
	// device's IO graph underneath the running output stream. From here
	// that looks like a device that is "open" but never asks for samples
	// again: cues queue up and are never heard. The callback stamps lastCB
	// every period (25ms), so a stamp older than deadAfter with a device
	// supposedly live means the stream is gone. Rebuild it off this
	// goroutine (Reopen retries and can take a while) and re-queue the cue
	// once it is back, so the user still hears something — late beats never.
	if last := p.lastCB.Load(); last != 0 && time.Since(time.Unix(0, last)) > deadAfter {
		if p.logf != nil {
			p.logf("audio: output stream silent for %s — reopening", time.Since(time.Unix(0, last)).Round(time.Millisecond))
		}
		// Queue first (Reopen keeps p.active), then rebuild off this goroutine.
		p.mu.Lock()
		p.active = append(p.active, voice{buf: buf})
		p.mu.Unlock()
		go p.Reopen()
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dev.Load() == nil {
		return
	}
	p.active = append(p.active, voice{buf: buf})
}

// Close releases the device. Idempotent: a second Close is a no-op, and any
// Reopen that starts after (or is waiting on lifecycleMu during) the first
// Close returns without opening anything.
func (p *Player) Close() {
	if p == nil {
		return
	}
	p.StopWorking()
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	if p.closed.Swap(true) {
		return // already closed; the watchdog was stopped by the first call
	}
	if p.watchStop != nil {
		close(p.watchStop) // exactly once: guarded by the closed flag above
	}

	p.mu.Lock()
	dev, ctx := p.dev.Swap(nil), p.ctx
	p.ctx = nil
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

// ready reports whether cues can be queued right now (see working.go).
func (p *Player) ready() bool { return p.enabled && p.dev.Load() != nil }
