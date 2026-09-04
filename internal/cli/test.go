package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/sanke08/flowlite/internal/audio"
	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/speech"
	"github.com/sanke08/flowlite/internal/whisper"
)

var (
	testSeconds int
	testFile    string
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Record a few seconds (or read a WAV) and print the transcript — no hotkey, no paste",
	Long: `Proves the microphone → model → text pipeline on its own, with none of the
hotkey or permission machinery in the way. If this works and the hotkey does
not, the problem is the Accessibility permission (see: flowlite doctor).`,
	RunE: runTest,
}

func runTest(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	m, have := catalog.Get(cfg.Model)
	if !have || !m.Downloaded() {
		return fmt.Errorf("no model installed — run: flowlite setup")
	}
	path, _ := m.Path()

	fmt.Printf("loading %s… ", m.Label)
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
	fmt.Printf("%s (%s, %s)\n", ok("ready"), dev, time.Since(t0).Round(time.Millisecond))

	var samples []float32
	if testFile != "" {
		s, rate, err := audio.ReadWAV(testFile)
		if err != nil {
			return err
		}
		if rate != audio.SampleRate {
			return fmt.Errorf("%s is %d Hz; FlowLite needs 16000 Hz mono 16-bit WAV", testFile, rate)
		}
		samples = s
		fmt.Printf("read %s (%.1fs)\n", testFile, float64(len(s))/audio.SampleRate)
	} else {
		rec := audio.NewRecorder(cfg.InputDevice, testSeconds+1)
		fmt.Printf("recording for %ds — speak now\n", testSeconds)
		if err := rec.Start(); err != nil {
			return err
		}
		deadline := time.Now().Add(time.Duration(testSeconds) * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			fmt.Fprintf(os.Stderr, "\r  %s %4.1fs  ", meter(rec.Level()), time.Until(deadline).Seconds())
		}
		samples = rec.Stop()
		fmt.Fprintln(os.Stderr, "\r                                      ")
	}

	if !speech.HasSpeech(samples) {
		fmt.Println(warn("nothing heard") + dim(" — the level gate rejected the audio as silence/noise"))
		return nil
	}

	t0 = time.Now()
	segs, err := model.Transcribe(samples, whisper.Options{Language: cfg.Language})
	if err != nil {
		return err
	}
	took := time.Since(t0)
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

func init() {
	testCmd.Flags().IntVar(&testSeconds, "seconds", 4, "how long to record")
	testCmd.Flags().StringVar(&testFile, "file", "", "transcribe this 16 kHz mono WAV instead of recording")
	rootCmd.AddCommand(testCmd)
}
