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
	"github.com/sanke08/flowlite/internal/hotkey"
	"github.com/sanke08/flowlite/internal/inject"
	"github.com/sanke08/flowlite/internal/mainloop"
	"github.com/sanke08/flowlite/internal/overlay"
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

	mu    sync.Mutex
	state State
	// gen increments on every pill Show; a delayed Hide only fires if no
	// newer dictation has taken the pill over in the meantime.
	gen atomic.Uint64

	busy sync.Mutex // serialises transcriptions

	// NoPaste skips the paste keystroke: transcripts only go to Transcribed.
	NoPaste bool

	// Transcribed is called with each final text (used by `run` to echo).
	Transcribed func(text string, audioSeconds, took float64)
}

// New loads the model and opens the audio devices. It does not touch the
// keyboard yet; that happens in Run, on the main thread.
func New(cfg *config.Config, logger *log.Logger) (*Daemon, error) {
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

	d := &Daemon{
		cfg:     cfg,
		log:     logger,
		model:   model,
		rec:     audio.NewRecorder(cfg.InputDevice, cfg.MaxSeconds),
		player:  player,
		machine: hotkey.New(time.Duration(cfg.HoldThresholdMS) * time.Millisecond),
		events:  make(chan hotkey.KeyEvent, 64),
	}
	return d, nil
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
	d.log.Printf("listening — tap %s to start/stop, hold it to dictate, Esc to cancel",
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
	d.player.Close()
	overlay.Hide()
	d.model.Close()
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
	var e hotkey.Event
	if ev.Down {
		e = d.machine.Press(ev.Kind)
	} else {
		e = d.machine.Release(ev.Kind)
	}
	switch e {
	case hotkey.Start:
		d.start()
	case hotkey.Finish:
		d.finish()
	case hotkey.Cancel:
		d.cancel()
	}
}

func (d *Daemon) start() {
	if d.getState() != Idle {
		// Still transcribing the last one; the machine already moved on, so
		// put it back rather than leave it believing a session is open.
		d.machine.Reset()
		return
	}
	if err := d.rec.Start(); err != nil {
		d.log.Printf("microphone: %v", err)
		d.machine.Reset()
		d.show(overlay.Error, "Microphone unavailable")
		d.player.Play(sound.Error)
		d.hideAfter(holdError)
		return
	}
	d.setState(Recording)
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

	if !speech.HasSpeech(samples) {
		d.settle(overlay.Cancelled, "Nothing heard", sound.Cancel, holdCancelled)
		return
	}
	overlay.SetState(overlay.Transcribing, "Transcribing…")
	d.player.StartWorking()
	go d.transcribe(samples)
}

func (d *Daemon) cancel() {
	if d.getState() != Recording {
		return
	}
	d.rec.Cancel()
	d.setState(Transcribing) // block a new start until the pill settles
	d.settle(overlay.Cancelled, "Cancelled", sound.Cancel, holdCancelled)
}

func (d *Daemon) transcribe(samples []float32) {
	d.busy.Lock()
	defer d.busy.Unlock()

	t0 := time.Now()
	segs, err := d.model.Transcribe(samples, whisper.Options{Language: d.cfg.Language})
	d.player.StopWorking()
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
	if d.NoPaste {
		label = "Transcribed"
	} else if err := inject.Paste(text, d.cfg.RestoreClipboard); err != nil {
		d.log.Printf("paste failed: %v", err)
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

func (d *Daemon) show(s overlay.State, text string) {
	d.gen.Add(1)
	overlay.Show(s, text)
}

func (d *Daemon) hideAfter(delay time.Duration) {
	g := d.gen.Load()
	time.AfterFunc(delay, func() {
		if d.gen.Load() == g {
			overlay.Hide()
		}
	})
}

func (d *Daemon) tick() {
	if d.getState() != Recording {
		return
	}
	overlay.SetLevel(d.rec.Level())
	if d.rec.Duration() >= float64(d.cfg.MaxSeconds) || d.rec.Overflowed() {
		d.log.Printf("max duration (%ds) reached; stopping", d.cfg.MaxSeconds)
		d.machine.Reset()
		d.finish()
	}
}
