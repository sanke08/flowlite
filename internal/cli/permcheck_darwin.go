//go:build darwin

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/permissions"
)

// ownPermissions reports what macOS has granted the background daemon —
// FlowLite.app, i.e. flowlite itself — rather than the terminal this command
// runs in.
//
// A process asks macOS about its *responsible* app, and anything run from a
// shell is Terminal's. So the check runs where the daemon runs: FlowLite.app
// is opened once with `--perm-check FILE`, writes the answer and exits. With
// request it also shows the macOS prompts, so the Accessibility list gains the
// entry and the microphone question is asked as FlowLite.
func ownPermissions(request bool) (ax bool, mic permissions.MicStatus, err error) {
	if os.Getenv(backgroundEnv) == "1" {
		return permissions.Trusted(), permissions.Mic(), nil // already the daemon's context
	}
	if request {
		// An Accessibility row left by an earlier build is switched on in
		// System Settings but no longer matches this binary (the grant is
		// pinned to its code hash), and macOS does not prompt again while a
		// row exists — so the user is stuck with a switch that does nothing.
		// When the grant is not valid, drop FlowLite's row so the request
		// below adds a fresh one. A working grant is never touched.
		if ok, _, err := ownPermissions(false); err == nil && !ok {
			resetAccessibilityGrant()
		}
	}
	app, err := ensureAppBundle()
	if err != nil {
		return false, 0, err
	}
	d, err := config.Dir()
	if err != nil {
		return false, 0, err
	}
	out := filepath.Join(d, "permcheck.out")
	_ = os.Remove(out)
	defer os.Remove(out)

	args := []string{"-n", "-g", "-j", app, "--args", "--perm-check", out}
	if request {
		args = append(args, "--perm-check-request")
	}
	if o, err := exec.Command("open", args...).CombinedOutput(); err != nil {
		return false, 0, fmt.Errorf("open %s: %v: %s", appBundleName, err, strings.TrimSpace(string(o)))
	}

	// The microphone prompt waits for an answer (up to 120 s), so a request
	// gets the time to be answered; a plain check is near-instant.
	deadline := time.Now().Add(10 * time.Second)
	if request {
		deadline = time.Now().Add(130 * time.Second)
	}
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		b, err := os.ReadFile(out)
		if err != nil {
			continue
		}
		if ax, mic, ok := parsePermCheck(string(b)); ok {
			return ax, mic, nil
		}
	}
	return false, 0, fmt.Errorf("the permission check did not answer")
}

// resetAccessibilityGrant removes FlowLite's own Accessibility entry (by bundle
// id, so nothing else's) from macOS's privacy database. Best effort: if it
// fails the prompt simply behaves as it did before.
func resetAccessibilityGrant() {
	_ = exec.Command("tccutil", "reset", "Accessibility", appBundleID).Run()
}
