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
	dst := filepath.Join(app, "Contents", "MacOS", "flowlite")
	if fi, err := os.Lstat(dst); err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		t.Fatalf("executable must be a real file, not a symlink: %v %v", fi, err)
	}
	sum, err := fileSum(exe)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(app, "Contents", "Resources", "source")); string(got) != exe+"\n"+sum+"\n" {
		t.Fatalf("source stamp = %q, want %q", got, exe+"\n"+sum+"\n")
	}
	// The bundle must be validly signed as a whole, or macOS denies it the
	// microphone ("Failed to copy signing info").
	if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", app).CombinedOutput(); err != nil {
		t.Fatalf("codesign --verify: %v: %s", err, out)
	}
	if out, _ := exec.Command("codesign", "-d", "-r-", app).CombinedOutput(); !strings.Contains(string(out), `designated => identifier "`+appBundleID+`"`) {
		t.Fatalf("designated requirement not the stable one: %s", out)
	}
	// A second call leaves a correct bundle alone and still succeeds.
	before, _ := os.Stat(dst)
	if _, err := ensureAppBundle(); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(dst); !after.ModTime().Equal(before.ModTime()) {
		t.Error("an up-to-date bundle was rebuilt")
	}
}
