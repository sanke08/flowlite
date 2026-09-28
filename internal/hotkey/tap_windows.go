//go:build windows

package hotkey

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
	procGetCurrentThreadId  = kernel32.NewProc("GetCurrentThreadId")
)

const (
	whKeyboardLL = 13
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
	wmQuit       = 0x0012
)

type kbdllHookStruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

var (
	tapMu    sync.Mutex
	tapChord *chord
	tapOut   chan<- KeyEvent
	hook     uintptr
	hookProc = syscall.NewCallback(lowLevelKeyboardProc)
)

// ErrNotTrusted mirrors the macOS error; on Windows a hook failure is rare.
var ErrNotTrusted = errors.New("could not install the keyboard hook")

// Tap is a running global keyboard listener.
type Tap struct{}

// keyChange decodes a low-level hook message into a key name and direction.
func keyChange(wParam, lParam uintptr) (name string, down, ok bool) {
	kb := (*kbdllHookStruct)(unsafe.Pointer(lParam))
	down = wParam == wmKeyDown || wParam == wmSysKeyDown
	up := wParam == wmKeyUp || wParam == wmSysKeyUp
	if !down && !up {
		return "", false, false
	}
	name, ok = nameForWin[kb.vkCode]
	return name, down, ok
}

func lowLevelKeyboardProc(nCode int32, wParam, lParam uintptr) uintptr {
	if nCode == 0 {
		if name, down, ok := keyChange(wParam, lParam); ok {
			tapMu.Lock()
			c, out := tapChord, tapOut
			tapMu.Unlock()
			if c != nil && out != nil {
				if ev, ok := c.feed(name, down); ok {
					select {
					case out <- ev:
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
func StartTap(spec string, out chan<- KeyEvent) (*Tap, error) {
	if _, err := Parse(spec); err != nil {
		return nil, errors.New("unsupported key " + spec + ": " + err.Error())
	}
	tapMu.Lock()
	tapChord, tapOut = newChord(spec), out
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

// AllUp reports whether every key of the dictation chord is physically up.
func AllUp() bool {
	tapMu.Lock()
	c := tapChord
	tapMu.Unlock()
	return c == nil || c.allUp()
}

// ---- capture --------------------------------------------------------------

var (
	capMu       sync.Mutex
	capOut      chan<- RawKey
	captureProc = syscall.NewCallback(captureKeyboardProc)
)

func captureKeyboardProc(nCode int32, wParam, lParam uintptr) uintptr {
	if nCode == 0 {
		if name, down, ok := keyChange(wParam, lParam); ok {
			capMu.Lock()
			out := capOut
			capMu.Unlock()
			if out != nil {
				select {
				case out <- RawKey{Name: name, Down: down}:
				default:
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return r
}

// Capture is a running listener that reports every key, used while the user
// chooses the dictation key.
type Capture struct {
	tid  uintptr
	done chan struct{}
}

type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       struct{ x, y int32 }
	lPrivate uint32
}

// StartCapture begins reporting every key press and release to out. The hook
// gets a thread and message loop of its own, so it works without
// mainloop.Run.
func StartCapture(out chan<- RawKey) (*Capture, error) {
	capMu.Lock()
	capOut = out
	capMu.Unlock()

	c := &Capture{done: make(chan struct{})}
	opened := make(chan error)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(c.done)
		c.tid, _, _ = procGetCurrentThreadId.Call()
		h, _, err := procSetWindowsHookExW.Call(whKeyboardLL, captureProc, 0, 0)
		if h == 0 {
			opened <- errors.Join(ErrNotTrusted, err)
			return
		}
		defer procUnhookWindowsHookEx.Call(h)
		// Make sure the thread has a message queue before Stop can post
		// WM_QUIT to it; a thread gets one only on its first peek or get.
		var m msg
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 0)
		opened <- nil
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 { // WM_QUIT or an error
				return
			}
		}
	}()
	if err := <-opened; err != nil {
		return nil, err
	}
	return c, nil
}

// Stop ends the capture and waits for its hook to be removed.
func (c *Capture) Stop() {
	procPostThreadMessageW.Call(c.tid, wmQuit, 0, 0)
	<-c.done
	capMu.Lock()
	capOut = nil
	capMu.Unlock()
}
