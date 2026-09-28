//go:build windows

package cli

import (
	"os"
	"syscall"
)

var procFlushConsoleInputBuffer = syscall.NewLazyDLL("kernel32.dll").NewProc("FlushConsoleInputBuffer")

// drainInput throws away whatever is waiting to be read on stdin.
func drainInput() {
	procFlushConsoleInputBuffer.Call(os.Stdin.Fd())
}

// pollInput reports nothing on Windows: a low-level hook always sees the
// keyboard, so the capture never needs the console as a way out.
func pollInput() []byte { return nil }
