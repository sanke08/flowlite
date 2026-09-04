// Package sound gives the user their ears back: a dictation tool whose only
// feedback is a paste that may or may not happen feels broken even when it
// works. Every cue is synthesised — no asset files to ship or lose.
package sound

import "math"

const Rate = 44_100

// Cue is one of the app's audio signals.
type Cue int

const (
	Start   Cue = iota // mic is live — speak
	Stop               // key released; recording ended
	Working            // soft tick, repeated while transcribing
	Done               // text has been pasted
	Cancel             // Esc, or nothing worth transcribing
	Error              // something failed
)

func (c Cue) String() string {
	return [...]string{"start", "stop", "working", "done", "cancel", "error"}[c]
}

// note renders a tone with a short attack and exponential decay. A touch of
// second harmonic keeps a pure sine from sounding like a smoke alarm.
func note(freq, seconds, amp float64) []float32 {
	n := int(seconds * Rate)
	out := make([]float32, n)
	attack := Rate / 200 // 5 ms
	for i := 0; i < n; i++ {
		t := float64(i) / Rate
		env := math.Exp(-4.5 * t / seconds)
		if i < attack {
			env *= float64(i) / float64(attack)
		}
		v := math.Sin(2*math.Pi*freq*t) + 0.18*math.Sin(4*math.Pi*freq*t)
		out[i] = float32(amp * env * v)
	}
	return out
}

func silence(seconds float64) []float32 { return make([]float32, int(seconds*Rate)) }

func concat(parts ...[]float32) []float32 {
	var n int
	for _, p := range parts {
		n += len(p)
	}
	out := make([]float32, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

const (
	a3 = 220.00
	g4 = 392.00
	c5 = 523.25
	e5 = 659.25
	g5 = 783.99
)

var cache [6][]float32

// Samples returns the rendered cue (built once, then reused).
func Samples(c Cue) []float32 {
	if cache[c] != nil {
		return cache[c]
	}
	var s []float32
	switch c {
	case Start: // rising: "go ahead"
		s = concat(note(c5, 0.075, 0.26), note(e5, 0.11, 0.26))
	case Stop: // falling: "got it"
		s = concat(note(e5, 0.075, 0.24), note(c5, 0.11, 0.24))
	case Working: // quiet, unobtrusive tick
		s = note(g4, 0.035, 0.10)
	case Done: // bright, brief
		s = note(g5, 0.08, 0.22)
	case Cancel: // single low note
		s = note(a3, 0.12, 0.2)
	case Error: // double low buzz
		s = concat(note(a3, 0.09, 0.26), silence(0.06), note(a3, 0.09, 0.26))
	}
	cache[c] = s
	return s
}
