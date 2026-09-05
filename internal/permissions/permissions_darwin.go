//go:build darwin

// Package permissions answers "can this process see the keyboard?".
package permissions

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework ApplicationServices -framework Foundation -framework AVFoundation
#include <stdbool.h>
bool flowlite_ax_trusted(void);
bool flowlite_ax_request(void);
int  flowlite_mic_status(void);
bool flowlite_mic_request(void);
*/
import "C"

// Needed reports whether this OS gates keyboard monitoring at all.
func Needed() bool { return true }

// Trusted reports whether Accessibility has been granted to this process's
// responsible application (Terminal, when run from a shell).
func Trusted() bool { return bool(C.flowlite_ax_trusted()) }

// Request shows the system Accessibility prompt, which also registers the app
// in the Privacy list so the user only has to flip a switch. Returns the
// current state — almost always false on the first call.
func Request() bool { return bool(C.flowlite_ax_request()) }

// Mic reports whether the microphone has been granted. Unlike Accessibility,
// a missing microphone permission does not make recording fail — the device
// opens and returns silence — so nothing notices unless it is asked.
func Mic() MicStatus { return MicStatus(C.flowlite_mic_status()) }

// RequestMic shows the system microphone prompt and waits for the answer.
func RequestMic() bool { return bool(C.flowlite_mic_request()) }
