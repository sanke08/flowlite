package whisper

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sanke08/flowlite/internal/audio"
	"github.com/sanke08/flowlite/internal/speech"
)

// Runs against whichever downloaded model is found, using macOS `say` to
// make speech. Skips cleanly anywhere that is not possible.
func TestTranscribeRealModel(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("uses macOS `say` for test speech")
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Skip(err)
	}
	var model string
	for _, cand := range []string{
		"large-v3-turbo-q5/ggml-large-v3-turbo-q5_0.bin",
		"tiny.en/ggml-tiny.en.bin",
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
	if _, err := exec.LookPath("say"); err != nil {
		t.Skip("no `say`")
	}

	dir := t.TempDir()
	aiff := filepath.Join(dir, "t.aiff")
	wav := filepath.Join(dir, "t.wav")
	phrase := "Hey, so I want to build a local transcription tool that works on both Mac and Windows."
	if out, err := exec.Command("say", "-o", aiff, phrase).CombinedOutput(); err != nil {
		t.Skipf("say: %v %s", err, out)
	}
	if out, err := exec.Command("afconvert", "-f", "WAVE", "-d", "LEI16@16000", "-c", "1", aiff, wav).CombinedOutput(); err != nil {
		t.Skipf("afconvert: %v %s", err, out)
	}
	samples, rate, err := audio.ReadWAV(wav)
	if err != nil || rate != 16000 {
		t.Fatalf("ReadWAV: %v rate=%d", err, rate)
	}

	t0 := time.Now()
	m, err := Load(model)
	if err != nil {
		t.Fatalf("Load: %v\n%s", err, strings.Join(Logs(), "\n"))
	}
	defer m.Close()
	t.Logf("load: %s  metal=%v gpu=%q", time.Since(t0).Round(time.Millisecond), UsingMetal(), GPUName())

	// Warm-up pays for Metal pipeline compilation.
	if _, err := m.Transcribe(make([]float32, 16000), Options{}); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	t0 = time.Now()
	segs, err := m.Transcribe(samples, Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	el := time.Since(t0)
	text := speech.Finalise(segs)
	t.Logf("%.1fs audio -> %s (%.1fx realtime)", float64(len(samples))/16000, el.Round(time.Millisecond), float64(len(samples))/16000/el.Seconds())
	t.Logf("text: %s", text)

	for _, want := range []string{"local transcription", "Mac", "Windows"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Errorf("transcript missing %q: %q", want, text)
		}
	}
	if !UsingMetal() {
		t.Errorf("Metal did not initialise — running on CPU. Logs:\n%s", strings.Join(Logs(), "\n"))
	}

	// Silence must produce nothing pasteable.
	segs, _ = m.Transcribe(make([]float32, 8000), Options{})
	if got := speech.Finalise(segs); got != "" {
		t.Errorf("silence produced %q", got)
	}
}
