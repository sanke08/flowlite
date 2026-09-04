package speech

import (
	"math/rand"
	"testing"
)

func tone(amplitude float64, seconds float64) []float32 {
	rng := rand.New(rand.NewSource(0))
	n := int(SampleRate * seconds)
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(rng.NormFloat64() * amplitude)
	}
	return out
}

func TestHasSpeech(t *testing.T) {
	cases := []struct {
		name string
		in   []float32
		want bool
	}{
		{"digital silence", make([]float32, SampleRate), false},
		{"mic noise floor", tone(0.001, 1), false},
		{"room tone", tone(0.01, 1), false},
		{"speech level", tone(0.15, 1), true},
		{"mistap too short", tone(0.15, 0.1), false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		if got := HasSpeech(c.in); got != c.want {
			t.Errorf("%s: HasSpeech = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHallucinationsAreDropped(t *testing.T) {
	for _, in := range []string{" Thank you.", "Thanks for watching!", "[BLANK_AUDIO]", "  THANK YOU!!  "} {
		if got := Finalise([]string{in}); got != "" {
			t.Errorf("Finalise(%q) = %q, want empty", in, got)
		}
	}
}

func TestRealSpeechSurvives(t *testing.T) {
	want := "Ship the release on Friday."
	if got := Finalise([]string{want}); got != want {
		t.Errorf("got %q", got)
	}
}

func TestJoinSegments(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		// Whisper's inconsistent leading spaces weld sentences together.
		{[]string{"since Monday.", "After that"}, "since Monday. After that"},
		{[]string{" Hello", " world"}, "Hello world"},
		{[]string{"Hello", "", "  ", "world"}, "Hello world"},
	}
	for _, c := range cases {
		if got := JoinSegments(c.in); got != c.want {
			t.Errorf("JoinSegments(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClean(t *testing.T) {
	cases := map[string]string{
		"Hello (door closes) world.": "Hello world.",
		"[MUSIC] Let's begin.":       "Let's begin.",
		"too    many\n\nspaces":      "too many spaces",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}
