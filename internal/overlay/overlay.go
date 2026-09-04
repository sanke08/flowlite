// Package overlay is the floating pill: the only visible surface the daemon
// has. It must never take focus — the whole point is that text lands in the
// field the user was already typing in.
package overlay

// State selects the pill's appearance.
type State int

const (
	Hidden       State = iota
	Listening          // red dot, live waveform, elapsed time
	Transcribing       // blue pulsing dot, frozen dim bars
	Pasted             // green, brief
	Cancelled          // grey, brief
	Error              // red flash with a short reason
)

func (s State) String() string {
	return [...]string{"hidden", "listening", "transcribing", "pasted", "cancelled", "error"}[s]
}
