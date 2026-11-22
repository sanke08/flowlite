//go:build darwin && moonshine

package engine

import (
	"strings"

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/speech"
	"github.com/sanke08/flowlite/internal/transcribe"
)

func init() { catalog.EnableEngine(catalog.Transcribe) }

// MoonshineVersion is the transcribe.cpp release compiled in.
func MoonshineVersion() string { return transcribe.Version() }

// Moonshine is read in pieces of at most pieceMax, cut at pauses (see
// speech.SplitAtPauses): past about ten seconds its streaming models drop
// the last words of a piece, and the plain ones stop at 48 s. Measured on
// a 41 s recording, ~8 s pieces came back word-perfect.
const (
	pieceMin = 5 * speech.SampleRate
	pieceMax = 10 * speech.SampleRate
)

// A live stream is restarted at the first pause after streamSegMin, or
// regardless at streamSegMax: Moonshine Streaming re-reads its whole
// utterance on every decode, so an unbroken stream gets slower the longer
// the user talks — 9x slower than realtime by 40 s on the Small model.
const (
	streamSegMin = 6 * speech.SampleRate
	streamSegMax = 12 * speech.SampleRate
	streamGap    = speech.SampleRate * 3 / 10 // 300 ms of quiet counts as a pause
	gapFrame     = speech.SampleRate / 50     // judged 20 ms at a time
)

type moonEngine struct {
	m *transcribe.Model
}

// moonStreamer is a moonEngine whose model can stream.
type moonStreamer struct {
	*moonEngine
}

func loadTranscribe(path string) (Engine, error) {
	m, err := transcribe.Load(path)
	if err != nil {
		return nil, err
	}
	e := &moonEngine{m: m}
	if !m.CanStream() {
		return e, nil
	}
	// Warm the streaming path too, so the first live dictation does not pay
	// for building its graphs. Run (the daemon's own warm-up) covers the rest.
	if err := m.BeginStream(); err == nil {
		_ = m.Feed(make([]float32, speech.SampleRate))
		_, _ = m.Finalize()
	}
	return &moonStreamer{e}, nil
}

func (e *moonEngine) Transcribe(samples []float32, opt Options) (string, error) {
	pieces := speech.SplitAtPauses(samples, pieceMin, pieceMax)
	texts := make([]string, 0, len(pieces))
	for _, p := range pieces {
		// Silent stretches of a long recording are where Moonshine invents
		// words; skip them. A single piece is always run — it is either the
		// whole (already speech-gated) recording or a warm-up.
		if len(pieces) > 1 && !speech.HasSpeech(p) {
			continue
		}
		text, err := e.m.Run(p, opt.Abort.pointer())
		if err == transcribe.ErrAborted {
			return "", ErrAborted
		}
		if err != nil {
			return "", err
		}
		texts = append(texts, text)
	}
	return speech.Finalise(texts), nil
}

func (e *moonEngine) Device() string { return e.m.Device() }

func (e *moonEngine) Close() { e.m.Close() }

func (s *moonStreamer) NewStream(opt Options) (Stream, error) {
	if err := s.m.BeginStream(); err != nil {
		return nil, err
	}
	return &moonStream{m: s.m}, nil
}

// moonStream is one live transcription, kept as a run of short Moonshine
// streams: done holds the finished ones' text, the model the current one.
type moonStream struct {
	m       *transcribe.Model
	done    []string
	seg     int // samples fed to the current stream
	quiet   int // samples of quiet at the end of them
	full    string
	settled string
}

func (s *moonStream) Feed(samples []float32) error {
	if err := s.m.Feed(samples); err != nil {
		return err
	}
	s.seg += len(samples)
	for at := 0; at < len(samples); at += gapFrame {
		if speech.Quiet(samples[at:min(at+gapFrame, len(samples))]) {
			s.quiet += gapFrame
		} else {
			s.quiet = 0
		}
	}
	full, _ := s.m.Text()
	if full != s.full {
		s.settled = speech.AgreedPrefix(s.full, full)
		s.full = full
	}
	if (s.seg >= streamSegMin && s.quiet >= streamGap) || s.seg >= streamSegMax {
		return s.restart()
	}
	return nil
}

// restart finishes the current stream into done and begins a fresh one.
func (s *moonStream) restart() error {
	text, err := s.m.Finalize()
	if err != nil {
		return err
	}
	if text = strings.TrimSpace(text); text != "" {
		s.done = append(s.done, text)
	}
	s.seg, s.quiet, s.full, s.settled = 0, 0, "", ""
	return s.m.BeginStream()
}

func (s *moonStream) Text() (committed, tentative string) {
	parts := append(append([]string(nil), s.done...), s.settled)
	committed = speech.JoinSegments(parts)
	tentative = strings.TrimSpace(s.full[len(s.settled):])
	return committed, tentative
}

func (s *moonStream) Finish(rest []float32) (string, error) {
	if len(rest) > 0 {
		if err := s.m.Feed(rest); err != nil {
			return "", err
		}
	}
	text, err := s.m.Finalize()
	if err != nil {
		return "", err
	}
	parts := append(append([]string(nil), s.done...), text)
	s.done, s.full, s.settled = nil, "", ""
	return speech.Finalise(parts), nil
}

func (s *moonStream) Close() { s.m.EndStream() }
