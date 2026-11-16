//go:build !darwin

package cli

import (
	"errors"
	"os"
	"os/exec"

	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/permissions"
)

// spawnDaemon starts `flowlite --daemon` as a detached process logging to
// the log file, and returns without waiting for it. Used by a background
// daemon reloading itself.
func spawnDaemon() error {
	c, err := detachedDaemonCmd()
	if err != nil {
		return err
	}
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// spawnDaemonWatched starts the daemon for `flowlite start` and reports,
// through exited, whether it has already quit (during startup: it failed).
func spawnDaemonWatched() (exited func() bool, stop func(), err error) {
	c, err := detachedDaemonCmd()
	if err != nil {
		return nil, nil, err
	}
	if err := c.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() { _ = c.Wait(); close(done) }()
	exited = func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	return exited, func() {}, nil // never kill: the child is the daemon
}

func detachedDaemonCmd() (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	lp, _ := config.LogPath()
	logf, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	// The child gets its own copy of the descriptor at Start; ours is only
	// needed until then, and leaking one per start is harmless.
	c := exec.Command(exe, "--daemon")
	c.Stdout, c.Stderr = logf, logf
	detach(c)
	return c, nil
}

// migrateLegacyDaemon: only macOS ever ran the daemon as a launchd job.
func migrateLegacyDaemon() error { return errors.ErrUnsupported }

// removeDaemonJob has nothing to undo outside macOS.
func removeDaemonJob() {}

// ownPermissions: outside macOS the daemon's permissions are this process's.
func ownPermissions(request bool) (bool, permissions.MicStatus, error) {
	if request && !permissions.Trusted() {
		permissions.Request()
	}
	return permissions.Trusted(), permissions.Mic(), nil
}
