package cli

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/sanke08/flowlite/internal/audio"
	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/engine"
	"github.com/sanke08/flowlite/internal/mainloop"
	"github.com/sanke08/flowlite/internal/overlay"
	"github.com/sanke08/flowlite/internal/speech"
)

// livePreview is `flowlite --live-preview <edge>`: one pretend dictation with
// live transcription on — the pill listens while the caption fills in word by
// word (committed bright, tentative dim), then it transcribes and both fade.
// With out set, PNG snapshots of the caption are written there along the way.
// No microphone and no model are involved; it exists to see the caption.
func livePreview(pos, out string) error {
	if !overlay.ValidPosition(pos) {
		return fmt.Errorf("%q is not a position — choose one of: %s", pos, strings.Join(overlay.Positions, ", "))
	}
	overlay.SetPosition(pos)
	words := strings.Fields("so this is what live transcription looks like while you are still talking " +
		"the words you have already said settle in white and the last few the model is still unsure of stay grey " +
		"until it hears enough to commit to them and a long dictation keeps only its newest lines on screen")

	var snapErr error
	mainloop.Run(func() {
		defer mainloop.Stop()
		overlay.Show(overlay.Listening, "")
		t0 := time.Now()
		for i := 1; i <= len(words); i++ {
			cut := max(0, i-3) // the last three words are still tentative
			overlay.SetLiveText(strings.Join(words[:cut], " "), strings.Join(words[cut:i], " "))
			for end := time.Now().Add(180 * time.Millisecond); time.Now().Before(end); {
				t := time.Since(t0).Seconds()
				overlay.SetLevel(0.15 + 0.6*math.Abs(math.Sin(t*7))*(0.5+0.5*math.Sin(t*1.3)))
				time.Sleep(45 * time.Millisecond)
			}
			if out != "" && (i == 4 || i == 14 || i == len(words)) {
				if err := overlay.SnapshotCaption(filepath.Join(out, fmt.Sprintf("caption-%s-%02d.png", pos, i))); err != nil {
					snapErr = err
				}
			}
		}
		overlay.SetState(overlay.Transcribing, "")
		time.Sleep(900 * time.Millisecond)
		overlay.HideLiveText()
		overlay.SetState(overlay.Pasted, "")
		time.Sleep(600 * time.Millisecond)
	})
	return snapErr
}

// transcribeWAV is `flowlite --transcribe-wav <file>`: run a recording through
// a model exactly as the daemon would — the final pass, and the live preview
// it would have shown while the file was being spoken — and print both with
// timings. No microphone, hotkey or pill; it is for checking engines and
// models. modelKey "" means the configured model.
func transcribeWAV(path, modelKey string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if modelKey == "" {
		modelKey = cfg.Model
	}
	m, have := catalog.Get(modelKey)
	if !have {
		return fmt.Errorf("no model %q in the catalog", modelKey)
	}
	if !m.Downloaded() {
		return fmt.Errorf("%s is not downloaded", m.Label)
	}
	samples, rate, err := audio.ReadWAV(path)
	if err != nil {
		return err
	}
	if rate != audio.SampleRate {
		return fmt.Errorf("%s is %d Hz; FlowLite needs %d Hz mono", path, rate, audio.SampleRate)
	}
	secs := float64(len(samples)) / audio.SampleRate

	t0 := time.Now()
	model, err := engine.Load(m)
	if err != nil {
		return err
	}
	defer model.Close()
	if _, err := model.Transcribe(make([]float32, audio.SampleRate), engine.Options{}); err != nil {
		return fmt.Errorf("warm-up: %w", err)
	}
	fmt.Printf("%s (%s) on %s, loaded in %s\n", m.Label, m.Engine.Name(), model.Device(), time.Since(t0).Round(time.Millisecond))

	t0 = time.Now()
	text, err := model.Transcribe(samples, engine.Options{Language: cfg.Language})
	if err != nil {
		return err
	}
	took := time.Since(t0)
	fmt.Printf("final   %.1fs audio in %s (%.0fx realtime)\n  %q\n", secs, took.Round(time.Millisecond), secs/took.Seconds(), text)

	var typed string
	if st, ok := model.(engine.Streamer); ok {
		typed, err = streamWAV(st, samples, cfg.Language)
	} else {
		typed, err = rereadWAV(model, samples, cfg.Language)
	}
	if err != nil {
		return err
	}
	if streamFinal != "" {
		text = streamFinal // typing mode ends on the stream's own text
	}
	back, insert := speech.Correction(typed, text)
	fmt.Printf("typing  %d chars in the field when you let go (%d live corrections' backspaces); on release: %d backspaces, then type %q\n",
		len([]rune(typed)), liveBack, back, insert)
	return nil
}

// liveBack counts the backspaces live corrections used, and streamFinal is
// a streaming model's own final text, for transcribeWAV.
var (
	liveBack    int
	streamFinal string
)

// streamWAV feeds the recording in the daemon's 100 ms steps and prints the
// caption whenever it changes.
func streamWAV(st engine.Streamer, samples []float32, lang string) (typed string, err error) {
	s, err := st.NewStream(engine.Options{Language: lang})
	if err != nil {
		return "", err
	}
	defer s.Close()
	step := audio.SampleRate / 10
	var worst, total time.Duration
	feeds := 0
	var lastC, lastT string
	fmt.Println("live    streaming, 100 ms feeds:")
	for off := 0; off < len(samples); off += step {
		chunk := samples[off:min(off+step, len(samples))]
		t0 := time.Now()
		if err := s.Feed(chunk); err != nil {
			return "", err
		}
		c, t := s.Text()
		var back int
		back, _, typed = speech.Converge(typed, c, speech.LiveMaxBack)
		liveBack += back
		d := time.Since(t0)
		total += d
		worst = max(worst, d)
		feeds++
		if c != lastC || t != lastT {
			fmt.Printf("  %5.1fs  +%-5s %s %s\n", float64(off+len(chunk))/audio.SampleRate,
				d.Round(time.Millisecond), c, dim("["+t+"]"))
			lastC, lastT = c, t
		}
	}
	if feeds == 0 {
		return "", errors.New("no audio")
	}
	t0 := time.Now()
	final, err := s.Finish(nil)
	if err != nil {
		return "", err
	}
	fmt.Printf("  stream's own final (what typing mode pastes) in %s:\n  %q\n", time.Since(t0).Round(time.Millisecond), final)
	streamFinal = final
	fmt.Printf("  feed time: mean %s, worst %s (budget 100 ms)\n", (total / time.Duration(feeds)).Round(time.Millisecond), worst.Round(time.Millisecond))
	return typed, nil
}

// rereadWAV replays the re-reading preview against the recording's own
// timeline: a pass on the audio "captured so far", then the next pass after
// as much audio as the daemon would have waited.
func rereadWAV(model engine.Engine, samples []float32, lang string) (typed string, err error) {
	fmt.Println("live    re-reading the recent audio:")
	const window = 25 * audio.SampleRate
	gap := 300 * time.Millisecond
	prev := ""
	for at := audio.SampleRate - int(gap.Seconds()*audio.SampleRate); at < len(samples); {
		at = min(at+int(gap.Seconds()*audio.SampleRate), len(samples))
		from := max(0, at-window)
		t0 := time.Now()
		text, err := model.Transcribe(samples[from:at], engine.Options{Language: lang})
		if err != nil {
			return "", err
		}
		d := time.Since(t0)
		if speech.Implausible(text, at-from) {
			text = dim("(dropped: a decoding loop, not speech)")
		} else if from == 0 {
			var back int
			back, _, typed = speech.Converge(typed, speech.AgreedPrefix(prev, text), speech.LiveMaxBack)
			liveBack += back
			prev = text
		}
		fmt.Printf("  %5.1fs  pass %-6s %s\n", float64(at)/audio.SampleRate, d.Round(time.Millisecond), text)
		gap = min(max(d, 300*time.Millisecond), 1500*time.Millisecond)
	}
	return typed, nil
}
