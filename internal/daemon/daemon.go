// Package daemon is the heart of `flowlite run`: it turns key gestures into
// recordings, recordings into text, and text into a paste — with a sound and
// a pill state at every step so the user always knows what is happening.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sanke08/flowlite/internal/audio"
	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/history"
	"github.com/sanke08/flowlite/internal/hotkey"
	"github.com/sanke08/flowlite/internal/inject"
	"github.com/sanke08/flowlite/internal/mainloop"
	"github.com/sanke08/flowlite/internal/overlay"
	"github.com/sanke08/flowlite/internal/permissions"
	"github.com/sanke08/flowlite/internal/sound"
	"github.com/sanke08/flowlite/internal/speech"
	"github.com/sanke08/flowlite/internal/whisper"
)

// State is where the daemon is in a dictation.
type State int

const (
	Idle State = iota
	Recording
	Transcribing
)

func (s State) String() string {
	return [...]string{"idle", "recording", "transcribing"}[s]
}

// How long a terminal pill state stays visible before fading.
const (
	holdPasted    = 700 * time.Millisecond
	holdCancelled = 600 * time.Millisecond
	holdError     = 1400 * time.Millisecond
)

// Daemon owns the model, microphone, sounds and hotkey for one session.
type Daemon struct {
	cfg *config.Config
	log *log.Logger

	model   *whisper.Model
	rec     *audio.Recorder
	player  *sound.Player
	machine *hotkey.Machine
	events  chan hotkey.KeyEvent
	tap     *hotkey.Tap
	hist    *history.Store // nil when the history file cannot be opened

	mu    sync.Mutex
	state State
	// gen increments on every pill Show; a delayed Hide only fires if no
	// newer dictation has taken the pill over in the meantime.
	gen atomic.Uint64
	// pillUp is true from Show until the matching Hide. A recording that was
	// never confirmed (a lone tap) has no pill, so finish must raise one
	// before setting its state.
	pillUp atomic.Bool
	// closing is set the moment shutdown starts, so a transcription already
	// running knows not to paste or touch the pill on its way out.
	closing atomic.Bool
	// recGen counts start() calls. A microphone open that fails hands its
	// error back asynchronously (see start); recGen lets that handler tell
	// whether the gesture it was opening for is still the current one, or
	// was already superseded by a newer press.
	recGen atomic.Uint64

	busy sync.Mutex // serialises transcriptions and paste-last against Close

	// NoPaste skips the paste keystroke: transcripts only go to Transcribed.
	NoPaste bool

	// Transcribed is called with each final text (used by `run` to echo).
	Transcribed func(text string, audioSeconds, took float64)
}

// New loads the model and opens the audio devices. It does not touch the
// keyboard yet; that happens in Run, on the main thread.
func New(cfg *config.Config, logger *log.Logger) (*Daemon, error) {
	// The microphone permission has to be settled before anything else: a
	// denied microphone does not make recording fail, it makes it return
	// silence, so without this the daemon starts, listens, shows the pill and
	// transcribes nothing — with no error anywhere. Ask now, while the user is
	// looking at the terminal, rather than on the first key press.
	if st := permissions.Mic(); st == permissions.MicUnknown {
		permissions.RequestMic()
	}
	if st := permissions.Mic(); !st.OK() {
		return nil, errors.New(st.Hint())
	}

	m, ok := catalog.Get(cfg.Model)
	if !ok || !m.Downloaded() {
		return nil, errors.New("no model installed — run `flowlite setup`")
	}
	path, err := m.Path()
	if err != nil {
		return nil, err
	}

	t0 := time.Now()
	model, err := whisper.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", m.Label, err)
	}
	// Warm-up pays for Metal pipeline compilation now, not on the first
	// real dictation.
	if _, err := model.Transcribe(make([]float32, audio.SampleRate), whisper.Options{}); err != nil {
		model.Close()
		return nil, fmt.Errorf("warm-up: %w", err)
	}
	dev := "CPU"
	if whisper.UsingMetal() {
		dev = "Metal GPU " + whisper.GPUName()
	}
	logger.Printf("model %s ready on %s in %s", m.Label, dev, time.Since(t0).Round(time.Millisecond))

	player, err := sound.NewPlayer(cfg.Sounds)
	if err != nil {
		logger.Printf("sounds disabled: %v", err)
	}

	hist, err := history.Open()
	if err != nil {
		logger.Printf("history disabled: %v", err)
	}

	// Pin the pill to the configured screen edge before it is ever shown.
	overlay.SetPosition(cfg.PillPosition)

	d := &Daemon{
		cfg:     cfg,
		log:     logger,
		model:   model,
		rec:     audio.NewRecorder(cfg.InputDevice, cfg.MaxSeconds),
		player:  player,
		machine: hotkey.New(time.Duration(cfg.HoldThresholdMS) * time.Millisecond),
		events:  make(chan hotkey.KeyEvent, 64),
		hist:    hist,
	}
	mainloop.OnWake(d.wake)
	return d, nil
}

// wake runs after the system wakes from sleep. Closing the lid suspends the
// output device the sound Player has held open since New; macOS does not
// hand it back in a working state on its own, so without this cues silently
// stop playing (or stutter) after the machine wakes until the process is
// restarted.
func (d *Daemon) wake() {
	d.player.Reopen()
}

// Run installs the hotkey and processes events until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	var tapErr error
	mainloop.DispatchSync(func() {
		d.tap, tapErr = hotkey.StartTap(d.cfg.Hotkey, d.events)
	})
	if tapErr != nil {
		return tapErr
	}
	d.log.Printf("listening — hold %s to talk, double-tap for hands-free (tap to stop), triple-tap to paste the last transcript, Esc to cancel",
		hotkey.Label(d.cfg.Hotkey))

	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			d.Close()
			return nil
		case ev := <-d.events:
			d.handle(ev)
		case <-tick.C:
			d.tick()
		}
	}
}

// Close releases everything. Safe to call more than once.
func (d *Daemon) Close() {
	if d.tap != nil {
		tap := d.tap
		d.tap = nil
		mainloop.DispatchSync(tap.Stop)
	}
	if d.rec.Recording() {
		d.rec.Cancel()
	}
	// Tell a transcription in flight to stop short of pasting. The user has
	// quit; dropping text into whatever window they switch to a second later
	// is worse than not pasting at all. It is still written to history, so a
	// triple tap or `flowlite last` gets it back.
	d.closing.Store(true)
	// Then wait for it, so whisper_free never lands under a running
	// whisper_full. Model.Close would block on its own lock anyway; taking
	// busy here also stops a queued transcribe starting against a dying model.
	d.busy.Lock()
	d.model.Close()
	d.busy.Unlock()
	// Sounds and the pill go last: transcribe reaches for both on its way out,
	// and closing them first is a race against a goroutine we just waited for.
	d.player.Close()
	overlay.Hide()
}

// ---- state --------------------------------------------------------------

func (d *Daemon) getState() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

func (d *Daemon) setState(s State) {
	d.mu.Lock()
	d.state = s
	d.mu.Unlock()
}

// State is the current phase, for `status`-style reporting.
func (d *Daemon) State() State { return d.getState() }

// ---- gestures -----------------------------------------------------------

func (d *Daemon) handle(ev hotkey.KeyEvent) {
	// Let time settle any pending gesture first so a press is judged
	// against an up-to-date machine (a tick may be up to 50 ms late).
	d.act(d.machine.Expire())
	if ev.Down {
		d.act(d.machine.Press(ev.Kind))
	} else {
		d.act(d.machine.Release(ev.Kind))
	}
}

func (d *Daemon) act(e hotkey.Event) {
	switch e {
	case hotkey.Start:
		d.start()
	case hotkey.HoldConfirmed, hotkey.StartHandsFree:
		d.confirm()
	case hotkey.Finish:
		d.finish()
	case hotkey.PasteLast:
		d.pasteLast()
	case hotkey.Discard:
		d.discard()
	case hotkey.Cancel:
		d.cancel()
	}
}

// start opens the microphone the instant the key goes down. The gesture is
// not known yet, so this is silent: no sound and no pill until confirm.
func (d *Daemon) start() {
	switch d.getState() {
	case Transcribing:
		// Still busy with the last one; the machine already moved on, so
		// put it back rather than leave it believing a session is open.
		d.machine.Reset()
		return
	case Recording:
		// A stale, never-confirmed recording (the machine missed an
		// Expire); replace it with this fresh gesture.
		d.rec.Cancel()
		d.setState(Idle)
	}
	// Opening the microphone touches CoreAudio/WASAPI and can take on the
	// order of 100ms — long enough that doing it here would stall the event
	// loop and delay every other key event behind it, on every single press.
	// Recording state is set optimistically instead, and the open runs in
	// its own goroutine; Recorder serialises Start against Stop/Cancel
	// internally, so a gesture that resolves before the device finishes
	// opening waits out that same brief window rather than every press
	// paying for it up front.
	gen := d.recGen.Add(1)
	d.setState(Recording)
	go func() {
		if err := d.rec.Start(); err != nil {
			d.log.Printf("microphone: %v", err)
			d.micFailed(gen)
		}
	}()
}

// micFailed rolls back a start() whose microphone never actually opened. gen
// guards against a newer gesture having already begun by the time the error
// comes back — that one is not this handler's to touch.
func (d *Daemon) micFailed(gen uint64) {
	if d.recGen.Load() != gen {
		return
	}
	d.machine.Reset()
	d.setState(Idle)
	d.show(overlay.Error, "Microphone unavailable")
	d.player.Play(sound.Error)
	d.hideAfter(holdError)
}

// confirm is the moment a hold or a double-tap makes the recording real:
// the Start cue plays and the pill appears. Audio captured since the first
// key-down is already in the recorder.
func (d *Daemon) confirm() {
	if d.getState() != Recording {
		d.machine.Reset()
		return
	}
	d.player.Play(sound.Start)
	d.show(overlay.Listening, "")
}

func (d *Daemon) finish() {
	if d.getState() != Recording {
		return
	}
	samples := d.rec.Stop()
	d.setState(Transcribing)
	d.player.Play(sound.Stop)
	if !d.pillUp.Load() {
		d.show(overlay.Listening, "")
	}

	if !speech.HasSpeech(samples) {
		// Silence and a revoked microphone are indistinguishable in the
		// samples, so check which one it was before blaming the user.
		if st := permissions.Mic(); !st.OK() {
			d.log.Printf("microphone: %s", st.Hint())
			d.settle(overlay.Error, "Microphone blocked", sound.Error, holdError)
			return
		}
		d.settle(overlay.Cancelled, "Nothing heard", sound.Cancel, holdCancelled)
		return
	}
	overlay.SetState(overlay.Transcribing, "Transcribing…")
	d.player.StartWorking()
	go d.transcribe(samples)
}

// discard drops a recording nobody was told about: a lone tap, or Esc
// before the gesture was confirmed. No sound, no pill.
func (d *Daemon) discard() {
	if d.getState() != Recording {
		return
	}
	d.rec.Cancel()
	d.setState(Idle)
}

func (d *Daemon) cancel() {
	if d.getState() != Recording {
		return
	}
	d.rec.Cancel()
	d.setState(Transcribing) // block a new start until the pill settles
	if !d.pillUp.Load() {
		d.show(overlay.Listening, "")
	}
	d.settle(overlay.Cancelled, "Cancelled", sound.Cancel, holdCancelled)
}

// pasteLast is the triple tap: paste the previous transcript again, for
// when the last one landed nowhere because no field had focus.
func (d *Daemon) pasteLast() {
	switch d.getState() {
	case Transcribing:
		return // the pill is busy; the machine is already idle
	case Recording:
		d.rec.Cancel() // the sliver captured during the taps
	}
	d.setState(Transcribing) // hold off a new start until the pill settles

	var last history.Entry
	var ok bool
	if d.hist != nil {
		last, ok = d.hist.Last()
	}
	if !ok {
		d.show(overlay.Error, "Nothing to paste yet")
		d.player.Play(sound.Error)
		d.hideAfter(holdError)
		d.setState(Idle)
		return
	}
	// inject.Paste blocks: ~50ms before the keystroke so the OS registers the
	// new clipboard owner, and up to 600ms after restoring the old clipboard
	// contents. Running that here would stall the daemon's whole event loop
	// — no key events processed, no ticks — for as long as it takes, so the
	// next hold-to-talk would only register once this triple tap's paste
	// finally finishes.
	go d.pasteLastNow(last)
}

func (d *Daemon) pasteLastNow(last history.Entry) {
	// Hold busy for the same reason transcribe does: Close must not return —
	// and let the process exit — while this is still mid-paste, or the
	// clipboard restore it's waiting on never gets to run.
	d.busy.Lock()
	defer d.busy.Unlock()
	if d.closing.Load() {
		// Shutting down. The entry is already in history; nothing new to
		// paste into whatever the user has switched to on their way out.
		d.setState(Idle)
		return
	}

	label := "Pasted again"
	if d.NoPaste {
		label = "Last transcript"
		if d.Transcribed != nil {
			d.Transcribed(last.Text, last.AudioSeconds, 0)
		}
	} else if err := inject.Paste(last.Text, d.cfg.RestoreClipboard); err != nil {
		d.log.Printf("paste failed: %v", err)
		d.show(overlay.Error, "Paste failed")
		d.player.Play(sound.Error)
		d.hideAfter(holdError)
		d.setState(Idle)
		return
	}
	d.log.Printf("pasted last transcript again (%d chars)", len(last.Text))
	d.show(overlay.Pasted, label)
	d.player.Play(sound.Done)
	d.hideAfter(holdPasted)
	d.setState(Idle)
}

func (d *Daemon) transcribe(samples []float32) {
	d.busy.Lock()
	defer d.busy.Unlock()

	t0 := time.Now()
	segs, err := d.model.Transcribe(samples, whisper.Options{Language: d.cfg.Language})
	d.player.StopWorking()
	if d.closing.Load() {
		// Shutting down. Keep the words, but do not paste them into whatever
		// has focus now, and do not raise the pill after it has been hidden.
		if err == nil {
			if text := speech.Finalise(segs); text != "" {
				d.remember(history.Entry{
					Time:         time.Now(),
					Text:         text,
					Pasted:       false,
					AudioSeconds: float64(len(samples)) / audio.SampleRate,
				})
				d.log.Printf("shutting down: %d chars kept in history, not pasted", len(text))
			}
		}
		return
	}
	if err != nil {
		d.log.Printf("transcription failed: %v", err)
		d.settle(overlay.Error, "Transcription failed", sound.Error, holdError)
		return
	}
	text := speech.Finalise(segs)
	if text == "" {
		d.settle(overlay.Cancelled, "Nothing heard", sound.Cancel, holdCancelled)
		return
	}
	took := time.Since(t0)
	secs := float64(len(samples)) / audio.SampleRate

	label := "Pasted"
	pasted := false
	var pasteErr error
	if d.NoPaste {
		label = "Transcribed"
	} else if pasteErr = inject.Paste(text, d.cfg.RestoreClipboard); pasteErr == nil {
		pasted = true
	}
	// Every transcript is remembered, pasted or not, so a triple tap or
	// `flowlite last` can recover it.
	d.remember(history.Entry{Time: time.Now(), Text: text, Pasted: pasted, AudioSeconds: secs})
	if pasteErr != nil {
		d.log.Printf("paste failed: %v", pasteErr)
		d.settle(overlay.Error, "Paste failed", sound.Error, holdError)
		return
	}
	d.log.Printf("%.1fs audio -> %d chars in %s", secs, len(text), took.Round(time.Millisecond))
	if d.Transcribed != nil {
		d.Transcribed(text, secs, took.Seconds())
	}
	d.settle(overlay.Pasted, label, sound.Done, holdPasted)
}

// settle shows a terminal pill state, plays its cue, and returns to Idle.
func (d *Daemon) settle(s overlay.State, text string, cue sound.Cue, hold time.Duration) {
	d.player.StopWorking()
	overlay.SetState(s, text)
	d.player.Play(cue)
	d.hideAfter(hold)
	d.setState(Idle)
}

func (d *Daemon) remember(e history.Entry) {
	if d.hist == nil {
		return
	}
	if err := d.hist.Append(e); err != nil {
		d.log.Printf("history: %v", err)
	}
}

func (d *Daemon) show(s overlay.State, text string) {
	d.gen.Add(1)
	d.pillUp.Store(true)
	overlay.Show(s, text)
}

func (d *Daemon) hideAfter(delay time.Duration) {
	g := d.gen.Load()
	time.AfterFunc(delay, func() {
		if d.gen.Load() == g {
			d.pillUp.Store(false)
			overlay.Hide()
		}
	})
}

func (d *Daemon) tick() {
	// Time is what tells a tap from a hold and a lone tap from the start
	// of a double-tap; the machine finds out here.
	d.act(d.machine.Expire())
	if d.getState() != Recording {
		return
	}
	if d.pillUp.Load() {
		overlay.SetLevel(d.rec.Level())
	}
	if d.rec.Duration() >= float64(d.cfg.MaxSeconds) || d.rec.Overflowed() {
		d.log.Printf("max duration (%ds) reached; stopping", d.cfg.MaxSeconds)
		d.machine.Reset()
		d.finish()
	}
}
