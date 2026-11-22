package speech

import "strings"

// Live transcription keeps re-hearing the same words as more audio arrives,
// and the model's guess at the last few changes from one hearing to the next.
// A word is treated as settled once two hearings in a row agree on it.

// AgreedPrefix is the part of cur that prev already said, word for word.
// Words are compared without case or trailing punctuation ("dog," and
// "dog." agree), and cur's last word never counts — it may be half heard.
// The prefix is cut from cur, so it carries cur's spelling and punctuation.
func AgreedPrefix(prev, cur string) string {
	pw, cw := strings.Fields(prev), strings.Fields(cur)
	n := 0
	for n < len(pw) && n < len(cw)-1 && SameWord(pw[n], cw[n]) {
		n++
	}
	if n == 0 {
		return ""
	}
	at := 0
	for i := 0; i < n; i++ {
		at += strings.Index(cur[at:], cw[i]) + len(cw[i])
	}
	return cur[:at]
}

// SameWord compares two words ignoring case and trailing punctuation.
func SameWord(a, b string) bool {
	trim := func(w string) string { return strings.ToLower(strings.TrimRight(w, ".,!?;:")) }
	return trim(a) == trim(b)
}

// LiveMaxBack bounds how much typed text one live correction may take back.
// It is sized to about a sentence: the largest revisions come when a
// Moonshine stream restarts at a pause and finalizes its last sentence, and
// rewriting that one sentence right there keeps typing going; a revision
// reaching further back waits for the final pass instead.
const LiveMaxBack = 200

// Converge is how to bring live-typed text in line with what the model now
// says is settled: delete back characters from the end of typed, then type
// insert, after which the field holds next.
//
// Typed words that settled still has exactly stay put and only new words are
// appended. When the model has revised a word already typed — punctuation
// included, since keeping the field close to the model's latest hearing is
// what leaves the final correction nearly nothing to do — everything from
// that word on is retyped, unless that
// would take more than maxBack backspaces, in which case nothing happens and
// the final pass fixes it. A settled text that has merely lost words (a
// shorter hearing) changes nothing either: words are never deleted just for
// going unheard once.
func Converge(typed, settled string, maxBack int) (back int, insert, next string) {
	tw, sw := strings.Fields(typed), strings.Fields(settled)
	i := 0
	for i < len(tw) && i < len(sw) && tw[i] == sw[i] {
		i++
	}
	if i == len(sw) {
		return 0, "", typed // nothing new, or settled is shorter
	}
	off := len(typed) // byte offset where retyping starts
	if i < len(tw) {
		off = wordStart(typed, i)
		if i > 0 {
			off = wordEnd(typed, i-1)
		}
		back = len([]rune(typed[off:]))
		if back > maxBack {
			return 0, "", typed
		}
	}
	insert = strings.Join(sw[i:], " ")
	if i > 0 {
		insert = " " + insert
	}
	return back, insert, typed[:off] + insert
}

// wordStart and wordEnd are byte offsets of the i-th whitespace-separated
// word of s.
func wordStart(s string, i int) int {
	at := 0
	for w := 0; ; w++ {
		for at < len(s) && s[at] == ' ' {
			at++
		}
		if w == i {
			return at
		}
		for at < len(s) && s[at] != ' ' {
			at++
		}
	}
}

func wordEnd(s string, i int) int {
	at := wordStart(s, i)
	for at < len(s) && s[at] != ' ' {
		at++
	}
	return at
}

// Correction is how to turn text already typed into final: how many
// characters to delete from the end, and what to type after.
func Correction(typed, final string) (backspaces int, insert string) {
	t, f := []rune(typed), []rune(final)
	n := 0
	for n < len(t) && n < len(f) && t[n] == f[n] {
		n++
	}
	return len(t) - n, string(f[n:])
}
