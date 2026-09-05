package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/sanke08/flowlite/internal/audio"
	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/speech"
	"github.com/sanke08/flowlite/internal/whisper"
)

// testSeconds is how long settings → Test microphone records.
const testSeconds = 4

// testMicrophone proves the microphone → model → text pipeline on its own,
// with none of the hotkey or permission machinery in the way. If this works
// and the hotkey does not, the problem is the Accessibility permission.
func testMicrophone(cfg *config.Config) error {
	m, have := catalog.Get(cfg.Model)
	if !have || !m.Downloaded() {
		return fmt.Errorf("no model installed — choose one under Speech model first")
	}
	path, _ := m.Path()

	spin := startSpinner("loading " + m.Label + "…")
	defer spin.Stop() // no-op after Done; matters only on the error returns
	t0 := time.Now()
	model, err := whisper.Load(path)
	if err != nil {
		return err
	}
	defer model.Close()
	if _, err := model.Transcribe(make([]float32, audio.SampleRate), whisper.Options{}); err != nil {
		return err
	}
	dev := "CPU"
	if whisper.UsingMetal() {
		dev = "Metal GPU, " + whisper.GPUName()
	}
	spin.Done(fmt.Sprintf("loading %s… %s (%s, %s)", m.Label, ok("ready"), dev, time.Since(t0).Round(time.Millisecond)))

	rec := audio.NewRecorder(cfg.InputDevice, testSeconds+1)
	defer rec.Close()
	fmt.Printf("recording for %ds — speak now\n", testSeconds)
	if err := rec.Start(); err != nil {
		return err
	}
	deadline := time.Now().Add(testSeconds * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintf(os.Stderr, "\r  %s %4.1fs  ", meter(rec.Level()), time.Until(deadline).Seconds())
	}
	samples := rec.Stop()
	fmt.Fprintln(os.Stderr, "\r                                      ")

	if !speech.HasSpeech(samples) {
		fmt.Println(warn("nothing heard") + dim(" — the level gate rejected the audio as silence/noise"))
		return nil
	}

	spin = startSpinner("transcribing…")
	t0 = time.Now()
	segs, err := model.Transcribe(samples, whisper.Options{Language: cfg.Language})
	took := time.Since(t0)
	spin.Stop()
	if err != nil {
		return err
	}
	text := speech.Finalise(segs)
	secs := float64(len(samples)) / audio.SampleRate
	fmt.Printf("%s %.1fs of audio in %s (%.1fx realtime)\n", ok("transcribed"), secs, took.Round(time.Millisecond), secs/took.Seconds())
	fmt.Println()
	if text == "" {
		fmt.Println(warn("(empty — the model heard only silence or a known filler)"))
	} else {
		fmt.Println("  " + bold(text))
	}
	return nil
}

func meter(level float64) string {
	const w = 24
	n := int(level * w)
	if n > w {
		n = w
	}
	s := make([]byte, w)
	for i := range s {
		if i < n {
			s[i] = '|'
		} else {
			s[i] = '.'
		}
	}
	return string(s)
}
