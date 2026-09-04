//go:build darwin

// Package mainloop owns the AppKit run loop.
//
// Everything that draws (the pill) or listens to the keyboard (the event tap)
// needs a Cocoa run loop on the process's main thread. The main goroutine is
// pinned to that thread at init, hands its work to other goroutines, and then
// blocks in [NSApp run]. Anything that must touch AppKit is funnelled back
// through Dispatch.
package mainloop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdint.h>
void flowlite_mainloop_prepare(void);
void flowlite_mainloop_run(void);
void flowlite_mainloop_stop(void);
void flowlite_mainloop_dispatch(uintptr_t handle);
*/
import "C"

import (
	"runtime"
	"runtime/cgo"
	"sync"
)

func init() {
	// Must happen before any other goroutine runs, so that the goroutine
	// calling Run really is on the process's first thread.
	runtime.LockOSThread()
}

var (
	runOnce sync.Once
	stopped chan struct{}
)

// Run prepares NSApplication, starts `ready` on its own goroutine and blocks
// in the Cocoa run loop until Stop is called. Call it from main only.
func Run(ready func()) {
	runOnce.Do(func() {
		stopped = make(chan struct{})
		C.flowlite_mainloop_prepare()
		go ready()
		C.flowlite_mainloop_run()
		close(stopped)
	})
}

// Stop ends the run loop from any goroutine.
func Stop() {
	C.flowlite_mainloop_stop()
}

// Dispatch runs fn on the main thread asynchronously.
func Dispatch(fn func()) {
	h := cgo.NewHandle(fn)
	C.flowlite_mainloop_dispatch(C.uintptr_t(h))
}

// DispatchSync runs fn on the main thread and waits for it.
func DispatchSync(fn func()) {
	done := make(chan struct{})
	Dispatch(func() {
		fn()
		close(done)
	})
	<-done
}

//export flowliteMainloopCall
func flowliteMainloopCall(h C.uintptr_t) {
	handle := cgo.Handle(h)
	handle.Value().(func())()
	handle.Delete()
}
