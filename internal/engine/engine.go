// Package engine puts every speech-to-text library FlowLite can run behind
// one small interface, so the daemon, `flowlite test` and `doctor` never
// need to know which library a model belongs to.
package engine

import (
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/whisper"
)

// Options control one transcription.
type Options struct {
	// Language is an ISO code such as "en", or "" to detect. Engines whose
	// models know one language ignore it.
	Language string
	// Abort, when set, cuts the run short once raised; Transcribe then
	// returns ErrAborted.
	Abort *Abort
}

// ErrAborted is returned by a Transcribe whose Options.Abort was raised.
var ErrAborted = errors.New("transcription aborted")

// Engine is one loaded model.
type Engine interface {
	// Transcribe runs a whole 16 kHz mono recording and returns the
	// finished text — "" when nothing worth pasting was heard.
	Transcribe(samples []float32, opt Options) (string, error)
	// Device says where inference runs, e.g. "Metal GPU Apple M3".
	Device() string
	// Close frees the model, waiting for any transcription in flight.
	Close()
}

// Streamer is an Engine whose model can transcribe audio as it arrives.
type Streamer interface {
	Engine
	NewStream(opt Options) (Stream, error)
}

// Stream is one live transcription. Not safe for concurrent use.
type Stream interface {
	// Feed appends newly captured audio.
	Feed(samples []float32) error
	// Text is the transcript so far: committed words will not change,
	// tentative ones are the model's current guess at the last few.
	Text() (committed, tentative string)
	// Finish feeds the last of the audio, decodes everything outstanding
	// and returns the finished text of the whole stream — "" when nothing
	// worth pasting was heard. The stream is spent afterwards; Close it.
	Finish(rest []float32) (string, error)
	// Close discards the stream.
	Close()
}

// Load opens a downloaded model with the engine it belongs to.
func Load(m catalog.Model) (Engine, error) {
	path, err := m.Path()
	if err != nil {
		return nil, err
	}
	switch m.Engine {
	case catalog.Whisper:
		return loadWhisper(path)
	case catalog.Transcribe:
		return loadTranscribe(path)
	}
	return nil, fmt.Errorf("%s: unknown engine %q", m.Label, m.Engine)
}

// Abort is a cancellation signal every engine can poll from its own threads.
type Abort struct {
	raised atomic.Bool
	w      *whisper.Flag
}

// NewAbort returns a lowered signal. Free it once no run can still use it.
func NewAbort() *Abort { return &Abort{w: whisper.NewFlag()} }

// Raise asks any run using this signal to stop. Safe from any goroutine.
func (a *Abort) Raise() {
	a.raised.Store(true)
	a.w.Raise()
}

// Raised reports whether Raise has been called.
func (a *Abort) Raised() bool { return a.raised.Load() }

// Free releases the signal.
func (a *Abort) Free() { a.w.Free() }

// pointer is the signal's C int, for C libraries to poll; nil for no signal.
func (a *Abort) pointer() unsafe.Pointer {
	if a == nil {
		return nil
	}
	return a.w.Pointer()
}
