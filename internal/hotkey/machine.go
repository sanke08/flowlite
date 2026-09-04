// Package hotkey turns raw key presses into dictation gestures.
//
// A single key drives two gestures. Recording always begins the instant the
// key goes down, so no speech is lost while we work out which one this is:
//
//	released quickly          -> a tap; keep recording until the next tap
//	held past the threshold   -> push-to-talk; stop on release
//
// Machine is pure Go with an injectable clock so it can be tested without an
// operating system in the loop. Platform taps (CGEventTap on macOS, a
// low-level keyboard hook on Windows) live in their own files and only feed
// Press/Release.
package hotkey

import "time"

// Gesture is where the machine is in a dictation.
type Gesture int

const (
	Idle      Gesture = iota
	Undecided         // key is down, too early to tell
	Toggle            // tapped; recording until the next tap
)

func (g Gesture) String() string {
	switch g {
	case Idle:
		return "idle"
	case Undecided:
		return "undecided"
	case Toggle:
		return "toggle"
	}
	return "?"
}

// Event is what the machine asks the daemon to do.
type Event int

const (
	None Event = iota
	Start
	Finish
	Cancel
)

func (e Event) String() string {
	switch e {
	case Start:
		return "start"
	case Finish:
		return "finish"
	case Cancel:
		return "cancel"
	}
	return "none"
}

// KeyKind classifies an incoming key for the machine.
type KeyKind int

const (
	Other KeyKind = iota
	Target
	Escape
)

// Machine is the tap-vs-hold state machine.
type Machine struct {
	threshold time.Duration
	now       func() time.Time

	state     Gesture
	keyDown   bool
	pressedAt time.Time
}

// New returns a machine that treats a press shorter than threshold as a tap.
func New(threshold time.Duration) *Machine {
	return &Machine{threshold: threshold, now: time.Now}
}

// State reports the current gesture.
func (m *Machine) State() Gesture { return m.state }

// Reset returns to idle without emitting anything. The daemon calls it when
// it stops a recording on its own (max duration reached); otherwise the
// machine would believe a toggle session was still open and swallow the
// user's next tap.
func (m *Machine) Reset() {
	m.state = Idle
	m.keyDown = false
}

// Press handles a key-down. Returns the event to act on, or None.
func (m *Machine) Press(k KeyKind) Event {
	switch k {
	case Escape:
		if m.state == Idle {
			return None
		}
		m.Reset()
		return Cancel
	case Target:
		if m.keyDown {
			return None // auto-repeat while held
		}
		m.keyDown = true
		if m.state == Toggle {
			// Second tap of a toggle session: finish it.
			m.state = Idle
			return Finish
		}
		m.state = Undecided
		m.pressedAt = m.now()
		return Start
	}
	return None
}

// Release handles a key-up. Returns the event to act on, or None.
func (m *Machine) Release(k KeyKind) Event {
	if k != Target || !m.keyDown {
		return None
	}
	m.keyDown = false
	if m.state != Undecided {
		return None // this release ended a toggle session's second tap
	}
	if m.now().Sub(m.pressedAt) >= m.threshold {
		m.state = Idle
		return Finish
	}
	m.state = Toggle // keep recording until the next tap
	return None
}
