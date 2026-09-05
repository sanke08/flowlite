//go:build !windows

package cli

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func terminate(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(syscall.SIGTERM)
}

// reload asks a running daemon to reload itself in place. It answers SIGHUP by
// replacing its own process image, so the change applies with nothing for the
// user to stop, start or think about.
func reload(pid int) error {
	return syscall.Kill(pid, syscall.SIGHUP)
}

// reexecSelf replaces this process with a fresh copy of the binary on disk.
// Same pid, same terminal, same stdin and stdout, same responsible app for the
// Accessibility grant — so a settings change or an update applies to a
// FlowLite running in someone's terminal tab without taking that tab away or
// downgrading it to a detached process that has to ask for permission again.
func reexecSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// watchStopRequest is a no-op here: SIGTERM already reaches the daemon
// through signal.NotifyContext. Windows needs a named event instead.
func watchStopRequest(stop func()) {}

// forceTerminate is not offered on Unix: SIGTERM is always deliverable, so a
// daemon that ignores it is reported rather than SIGKILLed mid-transcription.
func forceTerminate(pid int) error { return errors.ErrUnsupported }
