//go:build windows

// Windows main loop: a low-level keyboard hook only fires while the thread
// that installed it pumps messages, and the pill window needs a WndProc.
// Run owns that loop on the main thread; Dispatch queues closures and wakes
// the loop with a thread message so they execute there.
package mainloop

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procPostThreadMessage = user32.NewProc("PostThreadMessageW")
	procGetCurrentThread  = kernel32.NewProc("GetCurrentThreadId")
)

const (
	wmQuit = 0x0012
	wmApp  = 0x8000 // WM_APP: "drain the dispatch queue"
)

type point struct{ x, y int32 }

type msg struct {
	hwnd     uintptr
	message  uint32
	_        uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

var (
	runOnce sync.Once
	mainTID uint32
	queue   = make(chan func(), 256)
)

func init() { runtime.LockOSThread() }

// Run pumps messages on the main thread until Stop.
func Run(ready func()) {
	runOnce.Do(func() {
		tid, _, _ := procGetCurrentThread.Call()
		mainTID = uint32(tid)
		go ready()
		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 { // WM_QUIT or error
				return
			}
			if m.message == wmApp && m.hwnd == 0 {
				drain()
				continue
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	})
}

func drain() {
	for {
		select {
		case fn := <-queue:
			fn()
		default:
			return
		}
	}
}

// Stop ends the loop.
func Stop() {
	procPostThreadMessage.Call(uintptr(mainTID), wmQuit, 0, 0)
}

// Dispatch runs fn on the loop thread.
func Dispatch(fn func()) {
	queue <- fn
	procPostThreadMessage.Call(uintptr(mainTID), wmApp, 0, 0)
}

// DispatchSync runs fn on the loop thread and waits.
func DispatchSync(fn func()) {
	done := make(chan struct{})
	Dispatch(func() { fn(); close(done) })
	<-done
}

// ThreadID is the loop thread, for callers that need to install hooks on it.
func ThreadID() uint32 { return mainTID }
