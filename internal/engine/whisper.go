package engine

import (
	"errors"

	"github.com/sanke08/flowlite/internal/speech"
	"github.com/sanke08/flowlite/internal/whisper"
)

// whisperEngine runs Whisper models through whisper.cpp. Whisper reads a
// whole recording at a time, so it is not a Streamer: live transcription
// re-reads the recent audio instead (see daemon's live preview).
type whisperEngine struct {
	m *whisper.Model
}

func loadWhisper(path string) (Engine, error) {
	m, err := whisper.Load(path)
	if err != nil {
		return nil, err
	}
	return &whisperEngine{m: m}, nil
}

func (e *whisperEngine) Transcribe(samples []float32, opt Options) (string, error) {
	wo := whisper.Options{Language: opt.Language}
	if opt.Abort != nil {
		wo.Abort = opt.Abort.w
	}
	segs, err := e.m.Transcribe(samples, wo)
	if errors.Is(err, whisper.ErrAborted) {
		return "", ErrAborted
	}
	if err != nil {
		return "", err
	}
	return speech.Finalise(segs), nil
}

func (e *whisperEngine) Device() string {
	if whisper.UsingMetal() {
		return "Metal GPU " + whisper.GPUName()
	}
	return "CPU"
}

func (e *whisperEngine) Close() { e.m.Close() }
