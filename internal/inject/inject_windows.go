//go:build windows

package inject

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32             = syscall.NewLazyDLL("user32.dll")
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procOpenClipboard  = user32.NewProc("OpenClipboard")
	procCloseClipboard = user32.NewProc("CloseClipboard")
	procEmptyClipboard = user32.NewProc("EmptyClipboard")
	procGetClipData    = user32.NewProc("GetClipboardData")
	procSetClipData    = user32.NewProc("SetClipboardData")
	procSendInput      = user32.NewProc("SendInput")
	procGlobalAlloc    = kernel32.NewProc("GlobalAlloc")
	procGlobalLock     = kernel32.NewProc("GlobalLock")
	procGlobalUnlock   = kernel32.NewProc("GlobalUnlock")
	procGlobalSize     = kernel32.NewProc("GlobalSize")
	procClipSeqNumber  = user32.NewProc("GetClipboardSequenceNumber")
)

const (
	cfUnicodeText  = 13
	gmemMoveable   = 0x0002
	inputKeyboard  = 1
	keyEventFKeyUp = 0x0002
	vkControl      = 0x11
	vkV            = 0x56
)

var errRoundTrip = errors.New("clipboard read-back did not match")

// withClipboard retries briefly: another process may hold the clipboard open.
func withClipboard(fn func() error) error {
	for i := 0; i < 10; i++ {
		r, _, _ := procOpenClipboard.Call(0)
		if r != 0 {
			defer procCloseClipboard.Call()
			return fn()
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("could not open the Windows clipboard")
}

func clipboardGet() (string, bool) {
	var out string
	var ok bool
	_ = withClipboard(func() error {
		h, _, _ := procGetClipData.Call(cfUnicodeText)
		if h == 0 {
			return nil
		}
		p, _, _ := procGlobalLock.Call(h)
		if p == 0 {
			return nil
		}
		defer procGlobalUnlock.Call(h)
		// The clipboard owner's allocation can be any size — a hardcoded
		// slice length here would read past a smaller one into whatever
		// memory follows it, occasionally an unmapped page. GlobalSize is
		// the actual bound to build the slice against.
		size, _, _ := procGlobalSize.Call(h)
		n := int(size) / 2 // CF_UNICODETEXT is UTF-16: 2 bytes per unit
		if n == 0 {
			return nil
		}
		out = syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), n))
		ok = true
		return nil
	})
	return out, ok
}

func clipboardSet(text string) error {
	u, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	return withClipboard(func() error {
		procEmptyClipboard.Call()
		size := uintptr(len(u) * 2)
		h, _, _ := procGlobalAlloc.Call(gmemMoveable, size)
		if h == 0 {
			return errors.New("GlobalAlloc failed")
		}
		p, _, _ := procGlobalLock.Call(h)
		if p == 0 {
			return errors.New("GlobalLock failed")
		}
		copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u)), u)
		procGlobalUnlock.Call(h)
		r, _, _ := procSetClipData.Call(cfUnicodeText, h)
		if r == 0 {
			return errors.New("SetClipboardData failed")
		}
		return nil
	})
}

type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// INPUT is 40 bytes on x64: DWORD type, padding, then a 32-byte union.
type input struct {
	typ uint32
	_   uint32
	ki  keybdInput
	_   [8]byte
}

func pasteKeystroke() error {
	seq := []input{
		{typ: inputKeyboard, ki: keybdInput{wVk: vkControl}},
		{typ: inputKeyboard, ki: keybdInput{wVk: vkV}},
		{typ: inputKeyboard, ki: keybdInput{wVk: vkV, dwFlags: keyEventFKeyUp}},
		{typ: inputKeyboard, ki: keybdInput{wVk: vkControl, dwFlags: keyEventFKeyUp}},
	}
	n, _, err := procSendInput.Call(uintptr(len(seq)), uintptr(unsafe.Pointer(&seq[0])), unsafe.Sizeof(seq[0]))
	if int(n) != len(seq) {
		return errors.Join(errors.New("SendInput did not deliver the paste"), err)
	}
	return nil
}

// clipboardSerial is Windows' clipboard sequence number: it advances on every
// change, so comparing it before and after tells us whether what we put on the
// clipboard is still there. Zero means the call is unavailable, which compares
// equal to itself and so falls back to restoring unconditionally.
func clipboardSerial() uint64 {
	n, _, _ := procClipSeqNumber.Call()
	return uint64(n)
}
