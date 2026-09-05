// Package overlay is the floating pill: the only visible surface the daemon
// has. It must never take focus — the whole point is that text lands in the
// field the user was already typing in.
package overlay

import (
	"strings"
	"sync"
	"time"
)

// State selects the pill's appearance.
type State int

const (
	Hidden       State = iota
	Listening          // live waveform
	Transcribing       // bars settle into stubs with a sweeping shimmer
	Pasted             // nothing new: the pill fades out
	Cancelled          // nothing new: the pill fades out
	Error              // bars turn red, pulse twice, then fade out
)

func (s State) String() string {
	return [...]string{"hidden", "listening", "transcribing", "pasted", "cancelled", "error"}[s]
}

// Positions lists the screen edges the pill can sit on, in the order the
// CLI shows them.
var Positions = []string{"bottom", "top", "left", "right"}

var (
	posMu    sync.Mutex
	position = "bottom"
)

// ValidPosition reports whether pos names a screen edge.
func ValidPosition(pos string) bool {
	return positionCode(pos) >= 0
}

// SetPosition chooses the screen edge the pill appears on: "bottom" (the
// default), "top", "left" or "right". Unknown values fall back to "bottom".
// Takes effect the next time the pill is shown.
func SetPosition(pos string) {
	code := positionCode(pos)
	if code < 0 {
		code = 0
	}
	posMu.Lock()
	position = Positions[code]
	posMu.Unlock()
	applyPosition(code)
}

// Position returns the edge the pill is configured to sit on.
func Position() string {
	posMu.Lock()
	defer posMu.Unlock()
	return position
}

// positionCode maps an edge name to the index the platform layers use
// (0 bottom, 1 top, 2 left, 3 right), or -1.
func positionCode(pos string) int {
	pos = strings.ToLower(strings.TrimSpace(pos))
	for i, p := range Positions {
		if p == pos {
			return i
		}
	}
	return -1
}

// HistoryEntry is one row the history panel can show.
type HistoryEntry struct {
	Time time.Time
	Text string
}

// ShowHistory morphs the pill into a scrollable list of past transcripts.
// onPick is called (on some internal thread — callers must not assume any
// particular goroutine) with the index of a row the user clicked; onClose is
// called when the panel closes for any reason (Escape, a click outside it,
// or ShowHistory/HideHistory being called again) so the caller can update
// its own bookkeeping. Calling ShowHistory again while already open replaces
// the list and callbacks in place, it does not toggle closed — HideHistory
// is what closes it.
func ShowHistory(entries []HistoryEntry, onPick func(index int), onClose func()) {
	showHistory(entries, onPick, onClose)
}

// HideHistory morphs the panel back down to the plain pill (or fully hides
// if nothing else wants it shown).
func HideHistory() { hideHistory() }

// IsHistoryOpen reports whether the history panel is currently showing.
func IsHistoryOpen() bool { return isHistoryOpen() }
