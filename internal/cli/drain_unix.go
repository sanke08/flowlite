//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// pollInput returns whatever is waiting to be read on stdin, without
// blocking.
func pollInput() []byte {
	fd := int(os.Stdin.Fd())
	if err := syscall.SetNonblock(fd, true); err != nil {
		return nil
	}
	defer syscall.SetNonblock(fd, false)
	var out []byte
	buf := make([]byte, 256)
	for {
		n, err := syscall.Read(fd, buf)
		if n <= 0 || err != nil {
			return out
		}
		out = append(out, buf[:n]...)
	}
}

// drainInput throws away whatever is waiting to be read on stdin.
func drainInput() { pollInput() }
