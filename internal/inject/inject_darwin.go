//go:build darwin

package inject

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices
#include <stdbool.h>
#include <stdlib.h>
char *flowlite_clipboard_get(void);
bool  flowlite_clipboard_set(const char *text);
long  flowlite_clipboard_serial(void);
bool  flowlite_paste_keystroke(void);
*/
import "C"

import (
	"errors"
	"unsafe"
)

var errRoundTrip = errors.New("clipboard read-back did not match")

func clipboardGet() (string, bool) {
	p := C.flowlite_clipboard_get()
	if p == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p), true
}

func clipboardSet(text string) error {
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	if !bool(C.flowlite_clipboard_set(c)) {
		return errors.New("the clipboard would not accept the text")
	}
	return nil
}

// clipboardSerial is the pasteboard's change counter; it moves on every write.
func clipboardSerial() uint64 { return uint64(C.flowlite_clipboard_serial()) }

func pasteKeystroke() error {
	if !bool(C.flowlite_paste_keystroke()) {
		return errors.New("keyboard access is off — System Settings → Privacy & Security → Accessibility, switch on your terminal (or FlowLite), then restart it")
	}
	return nil
}
