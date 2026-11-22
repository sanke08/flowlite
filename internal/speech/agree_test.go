package speech

import "testing"

func TestAgreedPrefix(t *testing.T) {
	for _, c := range []struct{ prev, cur, want string }{
		{"", "Hello.", ""},
		{"Hello.", "Hello, this is", "Hello,"},
		{"Hello, this is", "Hello, this is a quick video.", "Hello, this is"},
		{"over the lazy dog,", "over the lazy dog. And", "over the lazy dog."},
		{"a quick test of 1.", "a quick test of life.", "a quick test of"},
		{"same words here", "same words here", "same words"},
	} {
		if got := AgreedPrefix(c.prev, c.cur); got != c.want {
			t.Errorf("AgreedPrefix(%q, %q) = %q, want %q", c.prev, c.cur, got, c.want)
		}
	}
}

func TestConverge(t *testing.T) {
	for _, c := range []struct {
		typed, settled string
		back           int
		insert, next   string
	}{
		{"", "Hello, this", 0, "Hello, this", "Hello, this"},
		{"Hello, this", "Hello, this is a", 0, " is a", "Hello, this is a"},
		// revised punctuation is retyped too
		{"lazy dog,", "lazy dog. And then", 5, " dog. And then", "lazy dog. And then"},
		// a revised word is retyped, and everything after it
		{"in Flow Lite", "in Flowlight. The quick", 10, " Flowlight. The quick", "in Flowlight. The quick"},
		{"test of life", "test of live transcription", 5, " live transcription", "test of live transcription"},
		{"Hello there", "Goodbye there", 11, "Goodbye there", "Goodbye there"},
		// nothing new, or a shorter hearing: leave it
		{"Hello there", "Hello there", 0, "", "Hello there"},
		{"Hello there", "Hello", 0, "", "Hello there"},
	} {
		back, insert, next := Converge(c.typed, c.settled, 80)
		if back != c.back || insert != c.insert || next != c.next {
			t.Errorf("Converge(%q, %q) = %d, %q, %q; want %d, %q, %q",
				c.typed, c.settled, back, insert, next, c.back, c.insert, c.next)
		}
	}
	// too far back to fix live: wait for the final pass
	if back, insert, next := Converge("one two three four", "uno two three four five", 5); back != 0 || insert != "" || next != "one two three four" {
		t.Errorf("long revision should wait, got %d %q %q", back, insert, next)
	}
}

func TestCorrection(t *testing.T) {
	for _, c := range []struct {
		typed, final string
		back         int
		ins          string
	}{
		{"Hello, this is", "Hello, this is a test.", 0, " a test."},
		{"over the lazy dog. And", "over the lazy dog, and then", 5, ", and then"},
		{"in Flow Lite", "in Flow Light.", 2, "ght."},
		{"héllo wörld", "héllo wörld", 0, ""},
		{"something", "", 9, ""},
	} {
		b, i := Correction(c.typed, c.final)
		if b != c.back || i != c.ins {
			t.Errorf("Correction(%q, %q) = %d, %q; want %d, %q", c.typed, c.final, b, i, c.back, c.ins)
		}
	}
}
