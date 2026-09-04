//go:build darwin

package overlay

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore
#include <stdlib.h>
#include <stdbool.h>
void flowlite_overlay_show(int state, const char *text);
void flowlite_overlay_set_state(int state, const char *text);
void flowlite_overlay_set_level(float level);
void flowlite_overlay_hide(void);
bool flowlite_overlay_snapshot(const char *path);
*/
import "C"

import (
	"errors"
	"unsafe"

	"github.com/sanke08/flowlite/internal/mainloop"
)

// Show makes the pill visible in the given state. Main-thread work is
// dispatched; callers may be on any goroutine.
func Show(s State, text string) {
	mainloop.Dispatch(func() {
		c := C.CString(text)
		defer C.free(unsafe.Pointer(c))
		C.flowlite_overlay_show(C.int(s), c)
	})
}

// SetState changes appearance without repositioning.
func SetState(s State, text string) {
	mainloop.Dispatch(func() {
		c := C.CString(text)
		defer C.free(unsafe.Pointer(c))
		C.flowlite_overlay_set_state(C.int(s), c)
	})
}

// SetLevel pushes a 0–1 microphone level into the waveform.
func SetLevel(level float64) {
	mainloop.Dispatch(func() { C.flowlite_overlay_set_level(C.float(level)) })
}

// Hide removes the pill.
func Hide() {
	mainloop.Dispatch(func() { C.flowlite_overlay_hide() })
}

// Snapshot renders the pill's current appearance to a PNG without capturing
// the screen. Used by `pill-demo --snapshot` to verify the UI in tests.
func Snapshot(path string) error {
	var ok bool
	mainloop.DispatchSync(func() {
		c := C.CString(path)
		defer C.free(unsafe.Pointer(c))
		ok = bool(C.flowlite_overlay_snapshot(c))
	})
	if !ok {
		return errors.New("could not render the pill")
	}
	return nil
}
