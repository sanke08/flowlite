// Package speech gates audio before inference and cleans the model's output.
//
// Whisper is trained on captioned video and will confidently caption silence:
// "Thank you.", "Thanks for watching!" and friends are its favourite
// inventions. A mis-tapped hotkey must never paste one of those, so audio is
// screened for actual speech before a model ever sees it, and a
// known-hallucination filter catches whatever slips past.
package speech

import (
	"math"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	// SampleRate is what whisper.cpp expects; capture is opened at it directly.
	SampleRate = 16_000

	// MinSeconds: shorter than this and there is nothing worth sending to the model.
	MinSeconds = 0.35

	// SpeechRMS was measured against a normal speaking voice, which peaks
	// around 0.2 RMS. Room tone and mic self-noise sit below 0.01, so this
	// clears the noise floor with an order of magnitude to spare while still
	// catching quiet speakers.
	SpeechRMS = 0.015

	frameSamples  = 1600 // 100 ms
	minLoudFrames = 3    // ~300 ms of actual sound
)

// Matched after lowercasing and stripping punctuation.
var hallucinations = map[string]struct{}{
	"": {}, "you": {}, "thank you": {}, "thanks": {}, "thank you very much": {},
	"thanks for watching": {}, "thank you for watching": {},
	"please subscribe": {}, "like and subscribe": {},
	"subtitles by the amara org community": {},
	"bye": {}, "bye bye": {}, "goodbye": {}, "okay": {}, "ok": {}, "so": {},
	"um": {}, "uh": {}, "hmm": {}, "mm": {},
	"blank audio": {}, "silence": {}, "music": {}, "applause": {}, "beep": {},
}

var (
	// "[BLANK_AUDIO]", "(door closes)", "[MUSIC]" — sound annotations.
	annotation = regexp.MustCompile(`[\[(][^\])]*[\])]`)
	spaces     = regexp.MustCompile(`\s+`)
)

// FrameRMS returns the RMS of each consecutive 100 ms frame.
func FrameRMS(audio []float32) []float64 {
	if len(audio) < frameSamples {
		if len(audio) == 0 {
			return []float64{0}
		}
		return []float64{rms(audio)}
	}
	n := len(audio) / frameSamples
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = rms(audio[i*frameSamples : (i+1)*frameSamples])
	}
	return out
}

func rms(a []float32) float64 {
	var sum float64
	for _, v := range a {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(a)))
}

// HasSpeech reports whether this buffer is worth running a model over.
func HasSpeech(audio []float32) bool {
	if float64(len(audio)) < MinSeconds*SampleRate {
		return false
	}
	loud := 0
	for _, r := range FrameRMS(audio) {
		if r > SpeechRMS {
			loud++
		}
	}
	return loud >= minLoudFrames
}

func normalise(s string) string {
	s = strings.ToLower(norm.NFKC.String(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || r == '_' {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(spaces.ReplaceAllString(b.String(), " "))
}

// IsHallucination reports whether the whole output is a known silence caption.
func IsHallucination(s string) bool {
	_, ok := hallucinations[normalise(s)]
	return ok
}

// JoinSegments joins model segments into one line.
//
// Whisper emits segments with inconsistent leading spaces, which is how
// "…since Monday." and "After that…" end up welded together.
func JoinSegments(segs []string) string {
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		if t := strings.TrimSpace(s); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// Clean tidies whitespace and drops bracketed sound annotations.
func Clean(s string) string {
	s = annotation.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "♪", " ")
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

// Finalise is the full post-processing pipeline: join, clean, reject
// hallucinations. Returns "" when nothing should be pasted.
func Finalise(segs []string) string {
	text := Clean(JoinSegments(segs))
	if IsHallucination(text) {
		return ""
	}
	return text
}
