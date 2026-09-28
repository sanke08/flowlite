//go:build darwin

package hotkey

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include <stdbool.h>
bool flowlite_tap_start(void);
void flowlite_tap_stop(void);
void flowlite_tap_clear_interest(void);
void flowlite_tap_add_interest(int keycode);
bool flowlite_capture_open(void);
void flowlite_capture_run(void);
void flowlite_capture_stop(void);
*/
import "C"

import (
	"errors"
	"runtime"
	"sync"
)

var (
	tapMu    sync.Mutex
	tapChord *chord
	tapOut   chan<- KeyEvent
)

// ErrNotTrusted is returned when macOS refuses to create the event tap,
// which in practice always means Accessibility has not been granted.
var ErrNotTrusted = errors.New("macOS refused the keyboard tap: Accessibility is not granted")

// Tap is a running global keyboard listener.
type Tap struct{}

// StartTap begins delivering events for the given chord. It must be called
// on the main thread (via mainloop.Dispatch) so the tap joins the main run
// loop.
func StartTap(spec string, out chan<- KeyEvent) (*Tap, error) {
	if _, err := Parse(spec); err != nil {
		return nil, errors.New("unsupported key " + spec + ": " + err.Error())
	}
	c := newChord(spec)
	tapMu.Lock()
	tapChord, tapOut = c, out
	tapMu.Unlock()

	C.flowlite_tap_clear_interest()
	for _, n := range c.interest() {
		if k, ok := lookup(n); ok {
			C.flowlite_tap_add_interest(C.int(k.mac))
		}
	}
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
	c := tapChord
	tapMu.Unlock()
	return c != nil && c.rightShift()
}

// AllUp reports whether every key of the dictation chord is physically up.
func AllUp() bool {
	tapMu.Lock()
	c := tapChord
	tapMu.Unlock()
	return c == nil || c.allUp()
}

//export flowliteTapEvent
func flowliteTapEvent(keycode C.int, down C.bool) {
	tapMu.Lock()
	c, out := tapChord, tapOut
	tapMu.Unlock()
	name, known := nameForMac[int(keycode)]
	if c == nil || out == nil || !known {
		return
	}
	ev, ok := c.feed(name, bool(down))
	if !ok {
		return // not ours; don't even wake the daemon
	}
	select {
	case out <- ev:
	default: // daemon is behind; dropping is safer than blocking the tap
	}
}

// ---- capture --------------------------------------------------------------

var (
	capMu  sync.Mutex
	capOut chan<- RawKey
)

// Capture is a running listener that reports every key, used while the user
// chooses the dictation key.
type Capture struct{ done chan struct{} }

// StartCapture begins reporting every key press and release to out. It runs
// its own run loop on a dedicated thread, so it works without mainloop.Run.
func StartCapture(out chan<- RawKey) (*Capture, error) {
	capMu.Lock()
	capOut = out
	capMu.Unlock()

	opened := make(chan bool)
	c := &Capture{done: make(chan struct{})}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(c.done)
		if !bool(C.flowlite_capture_open()) {
			opened <- false
			return
		}
		opened <- true
		C.flowlite_capture_run()
	}()
	if !<-opened {
		return nil, ErrNotTrusted
	}
	return c, nil
}

// Stop ends the capture and waits for its tap to be removed.
func (c *Capture) Stop() {
	C.flowlite_capture_stop()
	<-c.done
	capMu.Lock()
	capOut = nil
	capMu.Unlock()
}

//export flowliteCaptureEvent
func flowliteCaptureEvent(keycode C.int, down C.bool) {
	name, known := nameForMac[int(keycode)]
	if !known {
		return
	}
	capMu.Lock()
	out := capOut
	capMu.Unlock()
	if out == nil {
		return
	}
	select {
	case out <- RawKey{Name: name, Down: bool(down)}:
	default:
	}
}
