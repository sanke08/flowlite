//go:build windows

package cli

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008 /* DETACHED_PROCESS */}
}

func alive(pid int) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

func terminate(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// errNoReload means this platform cannot reload a daemon in place; callers
// fall back to stopping and starting it, which on Windows costs nothing —
// there is no Accessibility grant tied to the process here.
var errNoReload = errors.New("reload in place is not supported on Windows")

func reload(pid int) error { return errNoReload }

func reexecSelf() error { return errNoReload }
