package speech

import (
	"math"
	"testing"
)

// speechTone is "speech": a loud sine; gaps are silence.
func speechTone(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(0.3 * math.Sin(float64(i)*0.07))
	}
	return out
}

func TestSplitShortStaysWhole(t *testing.T) {
	s := speechTone(5 * SampleRate)
	got := SplitAtPauses(s, 4*SampleRate, 10*SampleRate)
	if len(got) != 1 || len(got[0]) != len(s) {
		t.Fatalf("got %d pieces", len(got))
	}
}

func TestSplitCutsInsideTheGap(t *testing.T) {
	// 7 s of speech, 0.5 s silence, 7 s of speech.
	var s []float32
	s = append(s, speechTone(7*SampleRate)...)
	gapAt := len(s)
	s = append(s, make([]float32, SampleRate/2)...)
	s = append(s, speechTone(7*SampleRate)...)

	got := SplitAtPauses(s, 4*SampleRate, 10*SampleRate)
	if len(got) != 2 {
		t.Fatalf("got %d pieces, want 2", len(got))
	}
	cut := len(got[0])
	if cut < gapAt || cut > gapAt+SampleRate/2 {
		t.Fatalf("cut at %.2fs, want inside the gap at %.2f–%.2fs",
			float64(cut)/SampleRate, float64(gapAt)/SampleRate, float64(gapAt+SampleRate/2)/SampleRate)
	}
	if len(got[0])+len(got[1]) != len(s) {
		t.Fatal("pieces do not cover the recording")
	}
}

func TestSplitWithoutPausesStillBounded(t *testing.T) {
	s := speechTone(35 * SampleRate)
	got := SplitAtPauses(s, 5*SampleRate, 10*SampleRate)
	total := 0
	for _, p := range got {
		if len(p) > 10*SampleRate {
			t.Fatalf("piece of %.1fs exceeds the max", float64(len(p))/SampleRate)
		}
		total += len(p)
	}
	if total != len(s) {
		t.Fatal("pieces do not cover the recording")
	}
}

func TestQuiet(t *testing.T) {
	if !Quiet(make([]float32, 1000)) || Quiet(speechTone(1000)) {
		t.Fatal("Quiet misjudged silence or tone")
	}
}
