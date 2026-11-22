package whisper

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// anyModel returns some downloaded Whisper model, or skips.
func anyModel(t *testing.T) string {
	t.Helper()
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Skip(err)
	}
	matches, _ := filepath.Glob(filepath.Join(cfg, "FlowLite", "models", "whispercpp", "*", "ggml-*.bin"))
	if len(matches) == 0 {
		t.Skip("no downloaded model")
	}
	return matches[0]
}

// A raised Abort must end the run with ErrAborted, and must not stick to the
// model: the next run without it has to work normally.
func TestAbortStopsTranscription(t *testing.T) {
	m, err := Load(anyModel(t))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	samples := make([]float32, 16000*5)

	f := NewFlag()
	defer f.Free()
	f.Raise()
	if _, err := m.Transcribe(samples, Options{Abort: f}); !errors.Is(err, ErrAborted) {
		t.Fatalf("raised flag: got %v, want ErrAborted", err)
	}
	if _, err := m.Transcribe(samples, Options{}); err != nil {
		t.Fatalf("run after an aborted one: %v", err)
	}
	g := NewFlag()
	defer g.Free()
	if _, err := m.Transcribe(samples, Options{Abort: g}); err != nil {
		t.Fatalf("lowered flag: %v", err)
	}
}
