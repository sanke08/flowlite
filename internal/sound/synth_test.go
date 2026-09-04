package sound

import (
	"math"
	"testing"
)

// You cannot judge a UI sound by listening in CI, but you can measure the
// things that make one sound cheap: clipping, clicks, DC, a tick that is not
// quiet enough to ignore, a cue that outstays its welcome.

func rms(s []float32) float64 {
	var acc float64
	for _, v := range s {
		acc += float64(v) * float64(v)
	}
	return math.Sqrt(acc / float64(len(s)))
}

func TestCuesAreWellFormed(t *testing.T) {
	// Duration windows in ms, from the design brief.
	dur := map[Cue][2]float64{
		Start:   {180, 260},
		Stop:    {180, 240},
		Working: {10, 40},
		Done:    {150, 220},
		Cancel:  {120, 180},
		Error:   {300, 400},
	}
	for c := Start; c <= Error; c++ {
		s := Samples(c)
		if len(s) == 0 {
			t.Fatalf("%s: empty", c)
		}
		ms := float64(len(s)) / Rate * 1000
		if w := dur[c]; ms < w[0] || ms > w[1] {
			t.Errorf("%s: duration %.1f ms outside [%v, %v]", c, ms, w[0], w[1])
		}
		if len(s) > int(0.5*Rate) {
			t.Errorf("%s: longer than 0.5 s", c)
		}

		var peak, mean, maxJump float64
		for i, v := range s {
			f := float64(v)
			if math.IsNaN(f) || math.IsInf(f, 0) {
				t.Fatalf("%s: non-finite sample at %d", c, i)
			}
			if a := math.Abs(f); a > peak {
				peak = a
			}
			mean += f
			if i > 0 {
				if j := math.Abs(f - float64(s[i-1])); j > maxJump {
					maxJump = j
				}
			}
		}
		mean /= float64(len(s))
		if peak > 0.5 {
			t.Errorf("%s: peak %.3f > 0.5", c, peak)
		}
		if peak < 0.02 {
			t.Errorf("%s: peak %.3f — effectively silent", c, peak)
		}
		if a := math.Abs(float64(s[0])); a >= 1e-3 {
			t.Errorf("%s: first sample %.4f not ~0", c, a)
		}
		if a := math.Abs(float64(s[len(s)-1])); a >= 1e-3 {
			t.Errorf("%s: last sample %.4f not ~0", c, a)
		}
		if math.Abs(mean) >= 1e-3 {
			t.Errorf("%s: DC offset %.5f", c, mean)
		}
		if maxJump > 0.15 {
			t.Errorf("%s: sample-to-sample jump %.3f > 0.15 (click)", c, maxJump)
		}
	}
}

func TestWorkingIsUnobtrusive(t *testing.T) {
	w, s := rms(Samples(Working)), rms(Samples(Start))
	if w > 0.12*s {
		t.Errorf("working RMS %.4f > 0.12 × start RMS %.4f", w, s)
	}
}

func TestSamplesAreCached(t *testing.T) {
	a, b := Samples(Done), Samples(Done)
	if &a[0] != &b[0] {
		t.Error("Samples re-rendered instead of returning the cached buffer")
	}
}
