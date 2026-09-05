//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procCreateEventW     = kernel32.NewProc("CreateEventW")
	procOpenEventW       = kernel32.NewProc("OpenEventW")
	procSetEvent         = kernel32.NewProc("SetEvent")
	procWaitForSingleObj = kernel32.NewProc("WaitForSingleObject")
	procResetEvent       = kernel32.NewProc("ResetEvent")
)

const (
	eventModifyState = 0x0002
	waitObject0      = 0
	waitInfinite     = 0xFFFFFFFF
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

// stopEventName is the named Win32 event a daemon waits on for a graceful
// shutdown request. Windows has no SIGTERM for a detached process — the only
// signal-like thing available is TerminateProcess, which gives the daemon no
// chance to finish a transcription in flight or restore the clipboard. So
// each daemon creates this event at startup and cancels its run context when
// it fires; terminate opens and sets it. "Local\" scopes it to this session.
func stopEventName(pid int) *uint16 {
	name, _ := syscall.UTF16PtrFromString(fmt.Sprintf(`Local\flowlite-stop-%d`, pid))
	return name
}

// watchStopRequest creates this process's stop event and calls stop when
// another flowlite sets it. Returns silently if the event cannot be created:
// the caller then falls back to TerminateProcess, exactly as before.
//
// The event is reset right after creation. Its name carries only the pid,
// and Windows reuses pids: if a previous daemon with this pid was stopped
// while some other process still held its event open, CreateEventW opens
// that existing, already-signalled event instead of making a new one, and a
// fresh daemon would shut down the instant it started listening.
func watchStopRequest(stop func()) {
	h, _, _ := procCreateEventW.Call(0, 1 /* manual reset */, 0, uintptr(unsafe.Pointer(stopEventName(os.Getpid()))))
	if h == 0 {
		return
	}
	procResetEvent.Call(h)
	go func() {
		r, _, _ := procWaitForSingleObj.Call(h, waitInfinite)
		if r == waitObject0 {
			stop()
		}
		syscall.CloseHandle(syscall.Handle(h))
	}()
}

// terminate asks the daemon to shut down cleanly by setting its stop event.
// A daemon too old to have created one cannot be reached that way, so it is
// killed outright; that escalation is reported as errForcedStop (the process
// is gone, but not gracefully) so callers can say so rather than pretend the
// stop was clean. If the event is set but the process lingers past the
// caller's wait, the caller escalates with forceTerminate itself.
func terminate(pid int) error {
	h, _, _ := procOpenEventW.Call(eventModifyState, 0, uintptr(unsafe.Pointer(stopEventName(pid))))
	if h == 0 {
		return forced(forceTerminate(pid))
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	if r, _, _ := procSetEvent.Call(h); r == 0 {
		return forced(forceTerminate(pid))
	}
	return nil
}

// forced maps a successful forceTerminate to errForcedStop and passes a
// failure through unchanged.
func forced(err error) error {
	if err != nil {
		return err
	}
	return errForcedStop
}

// forceTerminate is TerminateProcess: the fallback when the graceful path is
// unavailable or ignored. It waits briefly for the exit to register so the
// pidfile check that follows sees a dead process.
func forceTerminate(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Kill(); err != nil {
		return err
	}
	for range 20 {
		if !alive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("pid %d did not exit", pid)
}

// errNoReload means this platform cannot reload a daemon in place; callers
// fall back to stopping and starting it, which on Windows costs nothing —
// there is no Accessibility grant tied to the process here.
var errNoReload = errors.New("reload in place is not supported on Windows")

func reload(pid int) error { return errNoReload }

func reexecSelf() error { return errNoReload }
