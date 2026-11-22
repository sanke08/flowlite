package speech

import "math"

// Moonshine reads a recording best in short pieces: past roughly ten seconds
// its streaming models start dropping the last words, and every model gets
// slower per word the longer the piece. SplitAtPauses cuts a long recording
// into pieces of at most max samples, each cut placed in the quietest stretch
// it can find at least min samples into the piece, so a cut lands between
// words rather than through one.

const (
	splitFrame = SampleRate / 50 // 20 ms
	splitQuiet = 15              // frames averaged when judging a gap: 300 ms
)

// SplitAtPauses returns consecutive slices of samples (sharing its backing
// array) that together cover all of it. A recording no longer than max comes
// back whole.
func SplitAtPauses(samples []float32, min, max int) [][]float32 {
	if max <= 0 || len(samples) <= max {
		return [][]float32{samples}
	}
	if min <= 0 || min >= max {
		min = max / 2
	}
	var out [][]float32
	for len(samples) > max {
		cut := quietestCut(samples, min, max)
		out = append(out, samples[:cut])
		samples = samples[cut:]
	}
	return append(out, samples)
}

// quietestCut is the sample offset, between min and max, at the middle of
// the quietest splitQuiet-frame window.
func quietestCut(samples []float32, min, max int) int {
	first, last := min/splitFrame, max/splitFrame
	energy := make([]float64, last+1)
	for f := 0; f <= last; f++ {
		energy[f] = frameEnergy(samples, f*splitFrame)
	}
	best, bestAt := math.Inf(1), last
	for f := first; f+splitQuiet <= last+1; f++ {
		var sum float64
		for k := f; k < f+splitQuiet; k++ {
			sum += energy[k]
		}
		if sum < best {
			best, bestAt = sum, f+splitQuiet/2
		}
	}
	return bestAt * splitFrame
}

func frameEnergy(samples []float32, at int) float64 {
	end := at + splitFrame
	if end > len(samples) {
		end = len(samples)
	}
	var sum float64
	for _, s := range samples[at:end] {
		sum += float64(s) * float64(s)
	}
	return sum
}

// Quiet reports whether a stretch of audio has no speech in it, judged the
// same way HasSpeech judges a whole recording.
func Quiet(samples []float32) bool {
	if len(samples) == 0 {
		return true
	}
	var sum float64
	for _, s := range samples {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum/float64(len(samples))) < SpeechRMS
}

// maxCharsPerSecond is well past the fastest dictation (about 4 words, or
// 25 characters, a second).
const maxCharsPerSecond = 40

// Implausible reports whether text is too long to have been said in n
// samples — the mark of a model stuck repeating itself, not of speech.
func Implausible(text string, n int) bool {
	secs := float64(n) / SampleRate
	return float64(len([]rune(text))) > secs*maxCharsPerSecond+20
}
