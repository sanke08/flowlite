//go:build windows

package hotkey

import (
	"errors"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
)

const (
	whKeyboardLL = 13
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
	vkEscape     = 0x1B
)

// Virtual-key codes for the supported dictation keys.
var windowsVK = map[string]uint32{
	"ctrl_r":  0xA3, // VK_RCONTROL
	"alt_r":   0xA5, // VK_RMENU
	"shift_r": 0xA1, // VK_RSHIFT
	"f13":     0x7C,
	"f14":     0x7D,
	"f15":     0x7E,
}

type kbdllHookStruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

var (
	tapMu     sync.Mutex
	tapTarget uint32
	tapOut    chan<- KeyEvent
	hook      uintptr
	hookProc  = syscall.NewCallback(lowLevelKeyboardProc)
)

// ErrNotTrusted mirrors the macOS error; on Windows a hook failure is rare.
var ErrNotTrusted = errors.New("could not install the keyboard hook")

// Tap is a running global keyboard listener.
type Tap struct{}

func lowLevelKeyboardProc(nCode int32, wParam, lParam uintptr) uintptr {
	if nCode == 0 {
		kb := (*kbdllHookStruct)(unsafe.Pointer(lParam))
		down := wParam == wmKeyDown || wParam == wmSysKeyDown
		up := wParam == wmKeyUp || wParam == wmSysKeyUp
		if down || up {
			tapMu.Lock()
			target, out := tapTarget, tapOut
			tapMu.Unlock()
			if out != nil {
				var kind KeyKind
				switch kb.vkCode {
				case target:
					kind = Target
				case vkEscape:
					kind = Escape
				}
				if kind != Other {
					select {
					case out <- KeyEvent{Kind: kind, Down: down}:
					default:
					}
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return r
}

// StartTap installs the low-level hook. Must run on the message-loop thread
// (via mainloop.DispatchSync) so the hook keeps receiving events.
func StartTap(keyName string, out chan<- KeyEvent) (*Tap, error) {
	vk, ok := windowsVK[keyName]
	if !ok {
		return nil, errors.New("unsupported key: " + keyName)
	}
	tapMu.Lock()
	tapTarget, tapOut = vk, out
	tapMu.Unlock()

	h, _, err := procSetWindowsHookExW.Call(whKeyboardLL, hookProc, 0, 0)
	if h == 0 {
		return nil, errors.Join(ErrNotTrusted, err)
	}
	hook = h
	return &Tap{}, nil
}

// Stop removes the hook.
func (t *Tap) Stop() {
	if hook != 0 {
		procUnhookWindowsHookEx.Call(hook)
		hook = 0
	}
}
