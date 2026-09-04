// Package hotkey turns raw key presses into dictation gestures.
//
// A single key drives every gesture. Recording always begins the instant the
// key first goes down, silently, so no speech is lost while we work out which
// gesture this is:
//
//	held past the threshold      -> push-to-talk; stop on release
//	tap, tap, pause              -> hands-free; recording continues unheld
//	one press while hands-free   -> stop and transcribe
//	tap, tap, tap                -> paste the previous transcript again
//	a single tap                 -> nothing; the recording is discarded
//
// Hands-free is confirmed only once the tap window closes after the second
// tap, so a triple tap never flashes the Listening pill on its way to the
// paste.
//
// Machine is pure Go with an injectable clock so it can be tested without an
// operating system in the loop. Because a gesture is only known once a window
// of time has passed with nothing happening, the daemon must call Expire on a
// steady tick (50 ms is plenty). Platform taps (CGEventTap on macOS, a
// low-level keyboard hook on Windows) live in their own files and only feed
// Press/Release.
package hotkey

import "time"

// TapWindow is how long the machine waits after a tap for the next one before
// deciding the gesture is over.
const TapWindow = 300 * time.Millisecond

// Gesture is where the machine is in a dictation.
type Gesture int

const (
	Idle         Gesture = iota
	Undecided            // key is down, too early to tell
	Holding              // held past the threshold; push-to-talk until release
	Tapped               // one quick tap; waiting for a second within TapWindow
	DoubleTapped         // two quick taps; waiting out the window for a third
	HandsFree            // double-tap confirmed; recording with nothing held
)

func (g Gesture) String() string {
	switch g {
	case Idle:
		return "idle"
	case Undecided:
		return "undecided"
	case Holding:
		return "holding"
	case Tapped:
		return "tapped"
	case DoubleTapped:
		return "double-tapped"
	case HandsFree:
		return "hands-free"
	}
	return "?"
}

// Event is what the machine asks the daemon to do.
type Event int

const (
	None Event = iota
	// Start: the key went down; begin recording quietly. The gesture is not
	// known yet, so no sound and no pill.
	Start
	// HoldConfirmed: the key has been held past the threshold. This is
	// push-to-talk; let the user know the mic is live.
	HoldConfirmed
	// StartHandsFree: a second tap arrived and the window closed with no
	// third; keep recording with nothing held and let the user know the mic
	// is live.
	StartHandsFree
	// Finish: stop recording and transcribe what was captured.
	Finish
	// PasteLast: triple tap. Throw away the tiny recording and paste the
	// previous transcript again.
	PasteLast
	// Discard: a lone tap, or Esc before the gesture was confirmed. Drop the
	// recording without a sound.
	Discard
	// Cancel: Esc during a confirmed recording.
	Cancel
)

func (e Event) String() string {
	switch e {
	case Start:
		return "start"
	case HoldConfirmed:
		return "hold-confirmed"
	case StartHandsFree:
		return "start-hands-free"
	case Finish:
		return "finish"
	case PasteLast:
		return "paste-last"
	case Discard:
		return "discard"
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

// Machine is the tap/hold/multi-tap state machine.
type Machine struct {
	threshold time.Duration
	tapWindow time.Duration
	now       func() time.Time

	state      Gesture
	keyDown    bool
	pressedAt  time.Time
	releasedAt time.Time
}

// New returns a machine that treats a press shorter than threshold as a tap.
func New(threshold time.Duration) *Machine {
	return &Machine{threshold: threshold, tapWindow: TapWindow, now: time.Now}
}

// State reports the current gesture.
func (m *Machine) State() Gesture { return m.state }

// Reset returns to idle without emitting anything. The daemon calls it when
// it stops a recording on its own (max duration reached) or could not act on
// an event; otherwise the machine would believe a session was still open and
// misread the user's next press.
func (m *Machine) Reset() {
	m.state = Idle
	m.keyDown = false
}

// Press handles a key-down. Returns the event to act on, or None.
func (m *Machine) Press(k KeyKind) Event {
	switch k {
	case Escape:
		switch m.state {
		case Idle:
			return None
		case Undecided, Tapped, DoubleTapped:
			m.Reset()
			return Discard // nothing was ever confirmed to the user
		}
		m.Reset()
		return Cancel
	case Target:
		if m.keyDown {
			return None // auto-repeat while held
		}
		m.keyDown = true
		now := m.now()
		switch m.state {
		case Tapped:
			if now.Sub(m.releasedAt) <= m.tapWindow {
				// Second tap. Recording carries on from the first key-down;
				// hands-free is only confirmed once the window closes with
				// no third tap, so nothing is shown to the user yet.
				m.state = DoubleTapped
				return None
			}
			// Expire was not called in time; this is a fresh gesture and
			// the daemon's Start replaces the stale recording.
		case DoubleTapped:
			m.state = Idle
			if now.Sub(m.releasedAt) <= m.tapWindow {
				return PasteLast // third tap
			}
			// Expire was missed: hands-free was effectively running, so
			// this press stops it.
			return Finish
		case HandsFree:
			m.state = Idle
			return Finish
		}
		m.state = Undecided
		m.pressedAt = now
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
	now := m.now()
	switch m.state {
	case Undecided:
		if now.Sub(m.pressedAt) >= m.threshold {
			// Expire did not get to confirm the hold first; still a hold.
			m.state = Idle
			return Finish
		}
		m.state = Tapped
		m.releasedAt = now
	case Holding:
		m.state = Idle
		return Finish
	case DoubleTapped:
		// The second tap's release opens the window for a third.
		m.releasedAt = now
	}
	return None
}

// Expire lets time move the machine on. Call it on a steady tick: it confirms
// a hold once the threshold passes, discards a lone tap once the window
// closes, and confirms hands-free once a double tap's window closes with no
// third tap. Returns the event to act on, or None.
func (m *Machine) Expire() Event {
	now := m.now()
	switch m.state {
	case Undecided:
		if m.keyDown && now.Sub(m.pressedAt) >= m.threshold {
			m.state = Holding
			return HoldConfirmed
		}
	case Tapped:
		if now.Sub(m.releasedAt) > m.tapWindow {
			m.state = Idle
			return Discard
		}
	case DoubleTapped:
		if !m.keyDown && now.Sub(m.releasedAt) > m.tapWindow {
			m.state = HandsFree
			return StartHandsFree
		}
	}
	return None
}
