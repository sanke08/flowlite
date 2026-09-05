// Package sound gives the user their ears back: a dictation tool whose only
// feedback is a paste that may or may not happen feels broken even when it
// works. Every cue is synthesised — no asset files to ship or lose.
//
// The six cues are one instrument family: a small struck body (a few
// harmonics that die at different speeds, like a marimba bar or a soft bell)
// sitting on top of a felt 70 Hz sub layer, two slightly detuned layers for
// width, a whisper of room, and a warm tanh/low-pass finish tuned dark
// rather than bright — weight over sparkle. They all live in D major so that
// any two heard in a row sound like phrases of one voice rather than
// unrelated beeps.
package sound

import "math"

// Rate is the sample rate cues are rendered and played at. 48 kHz because
// that is what the built-in speakers (and nearly every modern Mac output)
// actually run at: matching it means miniaudio hands our buffers straight to
// CoreAudio instead of resampling 44.1k→48k inside the real-time callback.
const Rate = 48_000

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

// Pitches (D major) shared by every cue.
const (
	d2  = 73.42
	a2  = 110.00
	eb3 = 155.56
	a3  = 220.00
	d4  = 293.66
	a4  = 440.00
	d5  = 587.33
	fs5 = 739.99
	a5  = 880.00
	d6  = 1174.66
)

// partial is one overtone of a struck body: its frequency ratio to the
// fundamental, its level, and how quickly it fades (a time constant in
// seconds). Real bodies lose their high partials first; so do these.
type partial struct{ ratio, amp, decay float64 }

var (
	// mallet is the family's main timbre: warm, rounded, short sustain.
	// Fundamental and second partial carry more of the body now; the upper
	// partials are trimmed so the strike reads as weight, not glare.
	mallet = []partial{
		{1, 1.00, 0.170},
		{2, 0.30, 0.078},
		{3, 0.09, 0.032},
		{4.18, 0.03, 0.015}, // a touch inharmonic: "wood", not "organ"
	}
	// chime is brighter — a shimmering, quickly fading top end for Done —
	// but the shimmer is a garnish now, not the body: fundamental up, the
	// two topmost partials well down.
	chime = []partial{
		{1, 1.00, 0.135},
		{2, 0.42, 0.078},
		{2.99, 0.16, 0.028}, // detuned 3rd partial → soft shimmer beat
		{4.05, 0.07, 0.016},
		{6.3, 0.02, 0.009},
	}
	// dull is the low, damped body used for Cancel and Error.
	dull = []partial{
		{1, 1.00, 0.200},
		{2, 0.14, 0.050},
		{3, 0.03, 0.020},
	}
	// tick is modeled directly on Apple's own Tock.caf (the iOS
	// picker-wheel detent), measured from the actual file: not a tone at
	// all, but a dense inharmonic cluster around 1.8-3.6 kHz that peaks in
	// under a millisecond, then a much quieter buzzy tail lingering for
	// another ~18 ms. A single clean partial can never sound like this —
	// it's the density and inharmonicity that read as a mechanical click
	// instead of a note.
	tick = []partial{
		// the hit: broadband, dies within ~1-2 ms.
		{1.00, 1.00, 0.0009},
		{2.05, 0.55, 0.0008},
		{2.35, 0.45, 0.0007},
		{3.55, 0.30, 0.0006},
		{3.95, 0.22, 0.0005},
		// the tail: same cluster, much quieter, decays across the rest of
		// the note instead of dying with the hit.
		{2.20, 0.10, 0.0060},
		{3.70, 0.08, 0.0060},
	}
	// pure is a single sine, used for the sub-bass body.
	pure = []partial{{1, 1, 1}}
)

// note is a single struck note placed on the cue's timeline.
type note struct {
	freq     float64   // fundamental, Hz
	at       float64   // onset, seconds from the start of the cue
	dur      float64   // sounding length, seconds (release included)
	amp      float64   // level before mastering
	partials []partial // timbre
	attack   float64   // raised-cosine attack, seconds
	release  float64   // raised-cosine release at the end of dur, seconds
	glide    float64   // onset pitch multiplier (1 = none); settles with glideTau
	glideTau float64   // seconds; how fast the glide reaches the target pitch
	slide    float64   // pitch multiplier reached at the end of dur (1 = none)
	detune   float64   // cents between the two layered oscillators
	decay    float64   // scales every partial's decay time (1 = as written)
}

// render adds the note into buf (float64 for headroom during mixing).
func (v note) render(buf []float64) {
	start := int(v.at * Rate)
	n := int(v.dur * Rate)
	if start+n > len(buf) {
		n = len(buf) - start
	}
	if n <= 0 {
		return
	}
	if v.glideTau <= 0 {
		v.glideTau = 0.02
	}
	if v.decay <= 0 {
		v.decay = 1
	}
	if v.glide == 0 {
		v.glide = 1
	}
	if v.slide == 0 {
		v.slide = 1
	}
	spread := math.Pow(2, v.detune/1200/2) // ± half the detune on each layer
	var phA, phB float64
	for i := 0; i < n; i++ {
		t := float64(i) / Rate
		// Pitch: a fast onset glide (mallet "give") plus a slow slide across
		// the note (used for Cancel's downward sigh).
		mul := 1 + (v.glide-1)*math.Exp(-t/v.glideTau)
		mul *= 1 + (v.slide-1)*(t/v.dur)
		f := v.freq * mul
		phA += 2 * math.Pi * f * spread / Rate
		phB += 2 * math.Pi * f / spread / Rate

		env := 1.0
		if t < v.attack {
			env *= raisedCos(t / v.attack)
		}
		if rem := v.dur - t; rem < v.release {
			env *= raisedCos(rem / v.release)
		}
		var s float64
		for _, p := range v.partials {
			d := math.Exp(-t / (p.decay * v.decay))
			s += p.amp * d * (math.Sin(p.ratio*phA) + math.Sin(p.ratio*phB))
		}
		buf[start+i] += v.amp * env * 0.5 * s
	}
}

// raisedCos maps 0..1 to a smooth 0..1 (half a Hann window) — no corners,
// so no clicks.
func raisedCos(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	return 0.5 - 0.5*math.Cos(math.Pi*x)
}

// sub is the low body that gives a cue weight: a 70 Hz swell under a note.
// It used to be felt more than heard; now it's meant to be heard — a
// rounder, fuller low end is most of what makes these cues feel expensive.
func sub(at, amp float64) note {
	return note{freq: d2, at: at, dur: 0.16, amp: amp, partials: pure,
		attack: 0.014, release: 0.09, glide: 1.25, glideTau: 0.03}
}

// master is the finishing chain applied to every cue.
type master struct {
	cutoff float64 // one-pole low-pass, Hz
	drive  float64 // tanh saturation; ~1.0–1.5 is warmth, not distortion
	wet    float64 // room level, 0 = dry
	rt60   float64 // room decay to −60 dB, seconds
	peak   float64 // final peak level (full scale = 1)
}

func (m master) apply(x []float64) []float32 {
	saturate(x, m.drive)
	lowpass(x, m.cutoff)
	if m.wet > 0 {
		room(x, m.wet, m.rt60)
	}
	dcBlock(x)
	fadeEdges(x, 0.0015, 0.012)
	normalize(x, m.peak)
	out := make([]float32, len(x))
	for i, v := range x {
		out[i] = float32(v)
	}
	if len(out) > 0 {
		out[0], out[len(out)-1] = 0, 0
	}
	return out
}

func saturate(x []float64, drive float64) {
	if drive <= 0 {
		return
	}
	// Normalise first so the drive means the same thing for every cue.
	normalize(x, 0.8)
	k := 1 / math.Tanh(drive)
	for i, v := range x {
		x[i] = math.Tanh(v*drive) * k
	}
}

func lowpass(x []float64, cutoff float64) {
	a := 1 - math.Exp(-2*math.Pi*cutoff/Rate)
	var y float64
	for i, v := range x {
		y += a * (v - y)
		x[i] = y
	}
}

// room is a tiny Schroeder reverb: three damped feedback combs in parallel
// into one allpass. With ~10–20 ms delays and a short rt60 it reads as
// "this sound happened in a real space", not as an effect.
func room(x []float64, wet, rt60 float64) {
	dry := make([]float64, len(x))
	copy(dry, x)
	acc := make([]float64, len(x))
	for _, d := range []float64{0.0113, 0.0149, 0.0187} {
		comb(acc, dry, d, rt60)
	}
	allpass(acc, 0.0051, 0.5)
	for i := range x {
		x[i] = dry[i] + wet*acc[i]/3
	}
}

func comb(dst, src []float64, delay, rt60 float64) {
	n := int(delay * Rate)
	g := math.Pow(10, -3*delay/rt60)
	buf := make([]float64, len(src))
	var lp float64
	for i := range src {
		var fb float64
		if i >= n {
			fb = buf[i-n]
		}
		lp += 0.4 * (fb - lp) // damping: highs die faster than lows
		buf[i] = src[i] + g*lp
		dst[i] += buf[i]
	}
}

func allpass(x []float64, delay, g float64) {
	n := int(delay * Rate)
	in := make([]float64, len(x))
	copy(in, x)
	for i := range x {
		var xd, yd float64
		if i >= n {
			xd, yd = in[i-n], x[i-n]
		}
		x[i] = -g*in[i] + xd + g*yd
	}
}

// dcBlock is a 20 Hz one-pole high-pass: removes any offset the asymmetric
// saturation or the sub swell may have left.
func dcBlock(x []float64) {
	r := 1 - 2*math.Pi*20/Rate
	var px, py float64
	for i, v := range x {
		y := v - px + r*py
		px, py = v, y
		x[i] = y
	}
}

func fadeEdges(x []float64, in, out float64) {
	ni, no := int(in*Rate), int(out*Rate)
	for i := 0; i < ni && i < len(x); i++ {
		x[i] *= raisedCos(float64(i) / float64(ni))
	}
	for i := 0; i < no && i < len(x); i++ {
		x[len(x)-1-i] *= raisedCos(float64(i) / float64(no))
	}
}

func normalize(x []float64, peak float64) {
	var m float64
	for _, v := range x {
		if a := math.Abs(v); a > m {
			m = a
		}
	}
	if m == 0 {
		return
	}
	k := peak / m
	for i := range x {
		x[i] *= k
	}
}

// design is a cue's full recipe.
type design struct {
	length float64 // total rendered seconds, room tail included
	voices []note
	master
}

var designs = [6]design{
	// Start — "go ahead": a rising fourth (A4 → D5) with a soft sub swell
	// under the first note so it lands with weight, then room.
	// Start — the portamento swoop read as ambient hum, not "go": a start
	// cue needs a decisive onset, not a slow bloom. Back to a clearly
	// struck rising fourth (A3 → D4, octave down from the original) for
	// that, kept premium with wide chorus detune, a low boom with
	// kick-style pitch drop, a sustained bass pad, and more room.
	Start: {
		length: 0.250,
		voices: []note{
			{freq: d2, at: 0.000, dur: 0.220, amp: 1.0, partials: pure, attack: 0.007, release: 0.15, glide: 1.6, glideTau: 0.04},
			{freq: a2, at: 0.000, dur: 0.230, amp: 0.5, partials: dull, attack: 0.022, release: 0.12, detune: 4},
			{freq: a3, at: 0.000, dur: 0.150, amp: 0.9, partials: mallet, attack: 0.008, release: 0.05, glide: 0.985, detune: 8},
			{freq: d4, at: 0.070, dur: 0.170, amp: 1.0, partials: mallet, attack: 0.008, release: 0.05, glide: 0.985, detune: 8, decay: 1.3},
		},
		master: master{cutoff: 4000, drive: 1.45, wet: 0.34, rt60: 0.12, peak: 0.46},
	},
	// Stop — the mirror of Start, same treatment: a settling descending
	// fourth (D4 → A3, dropped an octave), a low boom under it with a
	// gentler pitch drop than Start's (settling, not launching), a
	// sustained bass pad, wide chorus detune, and a touch more room.
	Stop: {
		length: 0.235,
		voices: []note{
			{freq: d2, at: 0.000, dur: 0.200, amp: 0.95, partials: pure, attack: 0.007, release: 0.13, glide: 1.5, glideTau: 0.035},
			{freq: a2, at: 0.000, dur: 0.210, amp: 0.5, partials: dull, attack: 0.020, release: 0.11, detune: 4},
			{freq: d4, at: 0.000, dur: 0.140, amp: 0.85, partials: mallet, attack: 0.008, release: 0.05, glide: 1.012, detune: 8},
			{freq: a3, at: 0.065, dur: 0.155, amp: 1.0, partials: mallet, attack: 0.008, release: 0.06, glide: 1.012, detune: 8, decay: 1.3},
		},
		master: master{cutoff: 3800, drive: 1.4, wet: 0.32, rt60: 0.11, peak: 0.38},
	},
	// Working — a mechanical clock ticker. Two layers: the escapement click
	// (broadband tick partials at ~1.8 kHz, sub-millisecond attack) and a
	// short woody knock underneath (~620 Hz, mallet body, 14 ms) that gives
	// each tick the body of a real clock rather than a bare digital click.
	// Louder than the other cues' relative level would suggest because it
	// is short and repeats; no drive/glide/reverb, which would turn it into
	// a tone.
	Working: {
		length: 0.030,
		voices: []note{
			{freq: 1800, at: 0, dur: 0.012, amp: 1.0, partials: tick, attack: 0.0006, release: 0.003},
			{freq: 620, at: 0, dur: 0.016, amp: 0.7, partials: mallet, attack: 0.0008, release: 0.006, decay: 0.12},
		},
		master: master{cutoff: 6500, drive: 0, peak: 0.22},
	},
	// Done — the sparkle (A5 → D6, chime timbre, shimmering detuned third
	// partial) stays, but now sits on the same low boom + sustained pad
	// as Start/Stop instead of the old faint sub swell — the "ding" keeps
	// its brightness on top while gaining real weight underneath.
	Done: {
		length: 0.200,
		voices: []note{
			{freq: d2, at: 0.000, dur: 0.180, amp: 0.95, partials: pure, attack: 0.007, release: 0.11, glide: 1.5, glideTau: 0.035},
			{freq: a2, at: 0.000, dur: 0.190, amp: 0.45, partials: dull, attack: 0.018, release: 0.09, detune: 4},
			{freq: a5, at: 0.000, dur: 0.170, amp: 0.75, partials: chime, attack: 0.005, release: 0.04, glide: 0.99, detune: 8},
			{freq: d6, at: 0.045, dur: 0.155, amp: 0.85, partials: chime, attack: 0.005, release: 0.04, glide: 0.99, detune: 8},
			{freq: fs5, at: 0.045, dur: 0.155, amp: 0.40, partials: mallet, attack: 0.006, release: 0.04, detune: 5},
		},
		master: master{cutoff: 4300, drive: 1.4, wet: 0.30, rt60: 0.11, peak: 0.42},
	},
	// Cancel — a single low A3 that sighs downward a semitone: neutral,
	// "never mind", nothing to worry about.
	Cancel: {
		length: 0.150,
		voices: []note{
			sub(0, 0.40),
			{freq: a3, at: 0, dur: 0.135, amp: 1, partials: dull, attack: 0.012, release: 0.05, glide: 1.03, glideTau: 0.015, slide: 0.94, detune: 5},
		},
		master: master{cutoff: 2400, drive: 1.4, wet: 0.20, rt60: 0.08, peak: 0.34},
	},
	// Error — two dull low thuds descending A3 → E♭3 (a tritone, the one
	// interval that cannot resolve), each with a faint sub under it. Clearly
	// "no", but padded rather than buzzed.
	Error: {
		length: 0.360,
		voices: []note{
			sub(0.000, 0.80),
			{freq: a3, at: 0.000, dur: 0.170, amp: 1.0, partials: dull, attack: 0.010, release: 0.05, glide: 0.97, detune: 7},
			sub(0.150, 0.80),
			{freq: eb3, at: 0.150, dur: 0.190, amp: 1.0, partials: dull, attack: 0.010, release: 0.06, glide: 0.97, detune: 9, decay: 1.2},
		},
		master: master{cutoff: 1700, drive: 1.5, wet: 0.22, rt60: 0.10, peak: 0.42},
	},
}

func (d design) render() []float32 {
	buf := make([]float64, int(d.length*Rate))
	for _, v := range d.voices {
		v.render(buf)
	}
	return d.master.apply(buf)
}

var cache [6][]float32

// Samples returns the rendered cue (built once, then reused).
func Samples(c Cue) []float32 {
	if cache[c] != nil {
		return cache[c]
	}
	s := designs[c].render()
	cache[c] = s
	return s
}
