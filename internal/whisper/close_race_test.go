package whisper

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// Closing the model while a transcription is running used to call whisper_free
// on a context whisper_full was still reading: a use-after-free, reachable by
// pressing Ctrl+C mid-dictation. Close must wait instead.
func TestCloseDuringTranscribe(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("model paths are macOS-specific here")
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Skip(err)
	}
	var model string
	for _, cand := range []string{
		"base.en/ggml-base.en.bin",
		"tiny.en/ggml-tiny.en.bin",
		"large-v3-turbo-q5/ggml-large-v3-turbo-q5_0.bin",
	} {
		p := filepath.Join(cfg, "FlowLite", "models", "whispercpp", cand)
		if _, err := os.Stat(p); err == nil {
			model = p
			break
		}
	}
	if model == "" {
		t.Skip("no downloaded model")
	}

	m, err := Load(model)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// 20s of audio, so inference is unambiguously still running when Close lands.
	samples := make([]float32, 16000*20)
	for i := range samples {
		samples[i] = float32(i%400)/400*0.2 - 0.1
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		// Either it completes, or it reports the model is closed. It must not crash.
		if _, err := m.Transcribe(samples, Options{Language: "en"}); err != nil {
			t.Logf("Transcribe returned: %v (acceptable)", err)
		}
	}()
	go func() {
		defer wg.Done()
		m.Close()
	}()
	wg.Wait()

	// Close is idempotent, and a call afterwards must be refused, not crash.
	m.Close()
	if _, err := m.Transcribe(samples[:16000], Options{}); err == nil {
		t.Error("Transcribe on a closed model should error")
	}
}
