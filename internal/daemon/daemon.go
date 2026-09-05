// Package daemon is the heart of `flowlite run`: it turns key gestures into
// recordings, recordings into text, and text into a paste — with a sound and
// a pill state at every step so the user always knows what is happening.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
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
	micErr  chan uint64 // recGen of a start() whose microphone failed to open
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
		return nil, errors.New("no model installed — run `flowlite` to set one up")
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

	// Open the microphone (stopped) before the sound Player opens its output
	// stream: see audio.Recorder for why the capture unit's first open must
	// not happen while an output stream is live on the same device.
	rec := audio.NewRecorder(cfg.InputDevice, cfg.MaxSeconds)
	if err := rec.Prepare(); err != nil {
		logger.Printf("microphone: %v (will retry on first use)", err)
	}

	player, err := sound.NewPlayer(cfg.Sounds)
	if err != nil {
		logger.Printf("sounds disabled: %v", err)
	}
	player.SetLogger(logger.Printf)

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
		rec:     rec,
		player:  player,
		machine: hotkey.New(time.Duration(cfg.HoldThresholdMS) * time.Millisecond),
		events:  make(chan hotkey.KeyEvent, 64),
		micErr:  make(chan uint64, 4),
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
		case gen := <-d.micErr:
			d.micFailed(gen)
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
	// triple tap, or `flowlite settings` → "Recent transcripts", gets it back.
	d.closing.Store(true)
	// Then wait for it, so whisper_free never lands under a running
	// whisper_full. Model.Close would block on its own lock anyway; taking
	// busy here also stops a queued transcribe starting against a dying model.
	d.busy.Lock()
	d.model.Close()
	d.busy.Unlock()
	// busy coming free means any transcribe()/pasteLastNow() that was in
	// flight has fully returned — which means its inject.Paste call has
	// already queued a clipboard restore as a background goroutine (Paste no
	// longer blocks on that wait; see inject.go), but that goroutine may
	// still be mid-sleep. Give it a bounded chance to finish before sounds
	// and the pill go away and the process exits, or shutdown could leave
	// the transcript on the clipboard instead of whatever the user had
	// copied before dictating.
	inject.WaitPending(inject.RestoreDelay + 200*time.Millisecond)
	// Sounds and the pill go last: transcribe reaches for both on its way out,
	// and closing them first is a race against a goroutine we just waited for.
	d.player.Close()
	d.rec.Close()
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

// modifierHeld reports whether the history-panel chord's modifier is
// currently down. Meaningless (and forced false) when the dictation hotkey
// itself is Right Shift, since then "modifier held" would just mirror the
// hotkey's own down-state and every hold would misfire as history.
func (d *Daemon) modifierHeld() bool {
	if d.cfg.Hotkey == "shift_r" {
		return false
	}
	return hotkey.ModifierHeld()
}

func (d *Daemon) handle(ev hotkey.KeyEvent) {
	// Let time settle any pending gesture first so a press is judged
	// against an up-to-date machine (a tick may be up to 50 ms late).
	d.act(d.machine.Expire(d.modifierHeld()))
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
	case hotkey.ShowHistory:
		d.toggleHistory()
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
			// micFailed touches d.machine, which is documented as driven only
			// by the event-loop goroutine (see hotkey.Machine); hand off
			// through micErr instead of calling it from here directly.
			select {
			case d.micErr <- gen:
			default: // event loop is behind; dropping is safer than blocking
			}
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
	// inject.Paste still blocks synchronously for ~50ms before the keystroke,
	// so the OS registers the new clipboard owner, plus the keystroke and
	// clipboard set themselves — the up-to-600ms clipboard restore that used
	// to follow now runs in its own background goroutine (see inject.go) and
	// no longer costs Paste's caller anything. Running even that shorter,
	// still-blocking part here would stall the daemon's whole event loop —
	// no key events processed, no ticks — for as long as it takes, so the
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
			// last is a history.Entry read back from storage, which no longer
			// tracks how long the original recording was — pass 0 rather than
			// pretending we still know.
			d.Transcribed(last.Text, 0, 0)
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
				d.remember(history.Entry{Time: time.Now(), Text: text})
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
	var pasteErr error
	if d.NoPaste {
		label = "Transcribed"
	} else {
		pasteErr = inject.Paste(text, d.cfg.RestoreClipboard)
	}
	// Every transcript is remembered, pasted or not, so a triple tap, or
	// `flowlite settings` → "Recent transcripts", can recover it.
	d.remember(history.Entry{Time: time.Now(), Text: text})
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
	if d.hist == nil || !d.cfg.HistoryEnabled {
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

// toggleHistory handles the hold+Shift chord: the silent recording start()
// always begins on key-down is not what the user wanted here, so it must be
// cancelled the same way discard() cancels an unconfirmed one. It then opens
// (or, if already open, closes) the history panel — reachable only from
// Idle, so none of the pill's own gen/pillUp bookkeeping is involved.
func (d *Daemon) toggleHistory() {
	if d.getState() == Recording {
		d.rec.Cancel()
	}
	d.setState(Idle)

	if overlay.IsHistoryOpen() {
		overlay.HideHistory()
		return
	}
	if d.hist == nil {
		d.show(overlay.Error, "History unavailable")
		d.player.Play(sound.Error)
		d.hideAfter(holdError)
		return
	}
	// Note: d.cfg.HistoryEnabled gates whether remember() writes NEW entries
	// — it says nothing about whether existing history can be browsed, so it
	// is deliberately not checked here.
	entries, err := d.hist.List(50)
	if err != nil || len(entries) == 0 {
		d.show(overlay.Error, "No transcripts yet")
		d.player.Play(sound.Error)
		d.hideAfter(holdError)
		return
	}
	rows := make([]overlay.HistoryEntry, len(entries))
	for i, e := range entries {
		rows[i] = overlay.HistoryEntry{
			Time: e.Time,
			Text: historyPreviewText(e.Text),
		}
	}
	overlay.ShowHistory(rows, func(i int) {
		if i < 0 || i >= len(entries) {
			return
		}
		go d.pasteLastNow(entries[i])
	}, func() {})
}

// historyPreviewText collapses a transcript onto one clean logical line —
// folding embedded newlines and repeated whitespace from a dictated
// transcript down to single spaces — but no longer truncates it. The
// history-panel row now wraps onto multiple lines instead of the single
// non-wrapping line it used to be (see overlay_darwin.m's FLHistoryDataSource,
// whose preview NSTextField already caps visible height at
// HIST_ROW_MAXLINES with word-wrap and tail-truncation), so cutting the text
// here at a fixed rune count would just hide words that Cocoa has plenty of
// room to show. The full collapsed text is passed through; the wrapping,
// multi-line NSTextField's native ellipsis is now the only place a transcript
// is ever visually truncated.
//
// This mirrors settings_menu.go's oneLine helper but is duplicated rather
// than imported: internal/cli's oneLine is unexported, and internal/daemon
// has no business importing the CLI package for one small formatting helper.
// copyTranscript's own 60-rune cap for its CLI listing is unrelated and
// untouched by this.
func historyPreviewText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (d *Daemon) tick() {
	// Time is what tells a tap from a hold and a lone tap from the start
	// of a double-tap; the machine finds out here.
	d.act(d.machine.Expire(d.modifierHeld()))
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
