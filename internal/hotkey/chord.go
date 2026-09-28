package hotkey

import (
	"strings"
	"sync"
)

// chord turns raw per-key presses into Target presses and releases for the
// Machine. The chord counts as down once every member is held and as up the
// moment any member lets go. A chord of modifiers only goes down in whatever
// order its keys arrive; a chord with an ordinary key (Ctrl+Space) goes down
// only when that key is pressed with the modifiers already held, the way any
// shortcut works, so typing a space and then touching Control does nothing.
//
// Platform taps feed it from their callback thread; all methods lock.
type chord struct {
	mu      sync.Mutex
	members []string
	trigger string // the ordinary member, if any
	held    map[string]bool
	active  bool
	anyHeld int // how many members are physically down
	shiftR  bool
}

func newChord(spec string) *chord {
	c := &chord{members: strings.Split(spec, "+"), held: map[string]bool{}}
	for _, m := range c.members {
		if !isModifier(m) {
			c.trigger = m
		}
	}
	return c
}

func (c *chord) isMember(name string) bool {
	for _, m := range c.members {
		if m == name {
			return true
		}
	}
	return false
}

// interest is every key whose presses the chord needs to see.
func (c *chord) interest() []string {
	return append(append([]string{}, c.members...), "esc", "shift_r")
}

// feed takes one raw key change and returns the event for the Machine, if
// there is one.
func (c *chord) feed(name string, down bool) (KeyEvent, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name == "shift_r" {
		c.shiftR = down
	}
	if name == "esc" {
		return KeyEvent{Kind: Escape, Down: down}, true
	}
	if !c.isMember(name) {
		return KeyEvent{}, false
	}
	if down {
		if c.held[name] {
			return KeyEvent{}, false // auto-repeat
		}
		c.held[name] = true
		c.anyHeld++
		if c.active || c.anyHeld < len(c.members) {
			return KeyEvent{}, false
		}
		if c.trigger != "" && name != c.trigger {
			// Modifiers finished arriving after the ordinary key: not how a
			// shortcut is pressed, most likely typing. Wait for a fresh press
			// of the ordinary key.
			return KeyEvent{}, false
		}
		c.active = true
		return KeyEvent{Kind: Target, Down: true}, true
	}
	if !c.held[name] {
		return KeyEvent{}, false
	}
	c.held[name] = false
	c.anyHeld--
	if !c.active {
		return KeyEvent{}, false
	}
	c.active = false
	return KeyEvent{Kind: Target, Down: false}, true
}

// allUp reports whether no member of the chord is physically held.
func (c *chord) allUp() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.anyHeld == 0
}

func (c *chord) rightShift() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.shiftR
}

// Recorder works out which chord the user means from the keys they press
// while the dictation key is being chosen. Everything held at the same time
// counts; the biggest set held together in one go wins, and letting go of
// everything and pressing again starts over. Esc and Enter are left to the
// caller.
type Recorder struct {
	held  []string
	combo []string
}

// Feed takes one raw key change. It reports whether what should be shown
// changed.
func (r *Recorder) Feed(name string, down bool) bool {
	if k, ok := lookup(name); !ok || k.kind == kindReserved {
		return false
	}
	i := indexOf(r.held, name)
	if down {
		if i >= 0 {
			return false // auto-repeat
		}
		if len(r.held) == 0 {
			r.combo = nil // a fresh press replaces the previous choice
		}
		r.held = append(r.held, name)
		if len(r.held) >= len(r.combo) {
			r.combo = append(r.combo[:0:0], r.held...)
		}
		return true
	}
	if i < 0 {
		return false // went down before we were listening
	}
	r.held = append(r.held[:i], r.held[i+1:]...)
	return true
}

// Held is the chord spelling of what is down right now.
func (r *Recorder) Held() string { return Format(r.held) }

// Combo is the chord chosen so far, or "".
func (r *Recorder) Combo() string { return Format(r.combo) }

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
