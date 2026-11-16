//go:build darwin

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// FlowLite.app must be a valid, hidden bundle whose executable is this binary.
func TestAppBundle(t *testing.T) {
	t.Setenv("HOME", t.TempDir()+"/a & <b>")
	app, err := ensureAppBundle()
	if err != nil {
		t.Fatal(err)
	}
	pl := filepath.Join(app, "Contents", "Info.plist")
	if out, err := exec.Command("plutil", "-lint", pl).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v: %s", err, out)
	}
	for key, want := range map[string]string{
		"CFBundleIdentifier": appBundleID,
		"CFBundleExecutable": "flowlite",
		"LSUIElement":        "true",
	} {
		out, err := exec.Command("plutil", "-extract", key, "raw", pl).Output()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Errorf("%s = %q (%v), want %q", key, out, err, want)
		}
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	got, err := os.Readlink(filepath.Join(app, "Contents", "MacOS", "flowlite"))
	if err != nil || got != exe {
		t.Fatalf("executable link = %q (%v), want %q", got, err, exe)
	}
	// A second call leaves a correct bundle alone and still succeeds.
	if _, err := ensureAppBundle(); err != nil {
		t.Fatal(err)
	}
}
