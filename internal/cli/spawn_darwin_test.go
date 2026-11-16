//go:build darwin

package cli

import (
	"os/exec"
	"strings"
	"testing"
)

// The plist launchd is handed must parse, with paths that contain spaces
// ("Application Support") and characters that need XML escaping.
func TestLaunchdPlistIsValid(t *testing.T) {
	t.Setenv("HOME", t.TempDir()+"/a & <b>")
	t.Setenv("LANG", "en_US.UTF-8")
	p, err := writeLaunchdPlist()
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("plutil", "-lint", p).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v: %s", err, out)
	}
	out, err := exec.Command("plutil", "-extract", "ProgramArguments.1", "raw", p).Output()
	if err != nil || strings.TrimSpace(string(out)) != "--daemon" {
		t.Fatalf("ProgramArguments: %q %v", out, err)
	}
	out, err = exec.Command("plutil", "-extract", "EnvironmentVariables."+launchdEnv, "raw", p).Output()
	if err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Fatalf("%s: %q %v", launchdEnv, out, err)
	}
	out, _ = exec.Command("plutil", "-extract", "StandardOutPath", "raw", p).Output()
	if !strings.Contains(string(out), "a & <b>") {
		t.Fatalf("StandardOutPath lost the escaped path: %q", out)
	}
}
