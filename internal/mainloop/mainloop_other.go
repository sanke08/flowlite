//go:build !darwin && !windows

package mainloop

import "sync"

var (
	mu      sync.Mutex
	stopCh  chan struct{}
	runOnce sync.Once
)

// Run has no native loop to own here; it starts ready and blocks until Stop.
func Run(ready func()) {
	runOnce.Do(func() {
		stopCh = make(chan struct{})
		go ready()
		<-stopCh
	})
}

// Stop releases Run.
func Stop() {
	mu.Lock()
	defer mu.Unlock()
	select {
	case <-stopCh:
	default:
		close(stopCh)
	}
}

// Dispatch runs fn immediately; there is no main-thread requirement.
func Dispatch(fn func()) { fn() }

// DispatchSync runs fn immediately.
func DispatchSync(fn func()) { fn() }

// OnWake has no sleep/wake signal to hook here; fn is never called.
func OnWake(fn func()) {}
