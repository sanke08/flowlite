//go:build !darwin

package cli

import (
	"os"
	"os/exec"

	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/permissions"
)

// spawnDaemon starts `flowlite --daemon` as a detached process logging to
// the log file, and returns without waiting for it. Shared by `flowlite
// start` and by a background daemon reloading itself.
func spawnDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	lp, _ := config.LogPath()
	logf, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close() // the child holds its own descriptor after Start

	c := exec.Command(exe, "--daemon")
	c.Stdout, c.Stderr = logf, logf
	detach(c)
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// spawnedJobExited is only tracked on macOS, where launchd owns the daemon.
func spawnedJobExited() bool { return false }

// removeDaemonJob has nothing to undo outside macOS.
func removeDaemonJob() {}

// ownPermissions: outside macOS the daemon's permissions are this process's.
func ownPermissions(request bool) (bool, permissions.MicStatus, error) {
	if request && !permissions.Trusted() {
		permissions.Request()
	}
	return permissions.Trusted(), permissions.Mic(), nil
}
