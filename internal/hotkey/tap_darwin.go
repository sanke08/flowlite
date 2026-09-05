//go:build darwin

package hotkey

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include <stdbool.h>
bool flowlite_tap_start(void);
void flowlite_tap_stop(void);
*/
import "C"

import (
	"errors"
	"sync"
)

// Virtual keycodes (Carbon Events.h) for the supported dictation keys.
var darwinKeycodes = map[string]int{
	"alt_r":   61,
	"ctrl_r":  62,
	"cmd_r":   54,
	"shift_r": 60,
	"f13":     105,
	"f14":     107,
	"f15":     113,
}

const escapeKeycode = 53

var (
	tapMu     sync.Mutex
	tapTarget int
	tapOut    chan<- KeyEvent

	rightShiftHeld bool
)

// ErrNotTrusted is returned when macOS refuses to create the event tap,
// which in practice always means Accessibility has not been granted.
var ErrNotTrusted = errors.New("macOS refused the keyboard tap: Accessibility is not granted")

// Tap is a running global keyboard listener.
type Tap struct{}

// StartTap begins delivering events for the named key. It must be called on
// the main thread (via mainloop.Dispatch) so the tap joins the main run loop.
func StartTap(keyName string, out chan<- KeyEvent) (*Tap, error) {
	code, ok := darwinKeycodes[keyName]
	if !ok {
		return nil, errors.New("unsupported key: " + keyName)
	}
	tapMu.Lock()
	tapTarget, tapOut = code, out
	tapMu.Unlock()

	if !bool(C.flowlite_tap_start()) {
		return nil, ErrNotTrusted
	}
	return &Tap{}, nil
}

// Stop removes the tap.
func (t *Tap) Stop() { C.flowlite_tap_stop() }

// ModifierHeld reports whether Right Shift is currently held, for gestures
// that combine it with the dictation hotkey. Safe to call from any goroutine.
func ModifierHeld() bool {
	tapMu.Lock()
	defer tapMu.Unlock()
	return rightShiftHeld
}

//export flowliteTapEvent
func flowliteTapEvent(keycode C.int, down C.bool) {
	if int(keycode) == 60 { // right shift; tracked regardless of the configured target
		tapMu.Lock()
		rightShiftHeld = bool(down)
		tapMu.Unlock()
	}

	tapMu.Lock()
	target, out := tapTarget, tapOut
	tapMu.Unlock()
	if out == nil {
		return
	}
	var kind KeyKind
	switch int(keycode) {
	case target:
		kind = Target
	case escapeKeycode:
		kind = Escape
	default:
		return // not ours; don't even wake the daemon
	}
	select {
	case out <- KeyEvent{Kind: kind, Down: bool(down)}:
	default: // daemon is behind; dropping is safer than blocking the tap
	}
}
