//go:build darwin

package cli

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/permissions"
)

const permCheckLabel = "com.sanke08.flowlite.permcheck"

// ownPermissions reports what macOS has granted flowlite itself — the
// background daemon's permissions — rather than the terminal this command
// runs in.
//
// A process asks macOS about its *responsible* app, and anything run from a
// shell is Terminal's. The daemon runs under launchd, where flowlite is
// responsible for itself, so the only faithful check is to run one there: a
// one-shot launchd job (`flowlite --perm-check FILE`) that writes the answer
// and exits. With request it also shows the macOS prompts, so the Accessibility
// list gains a "flowlite" entry and the microphone question is asked as
// flowlite.
func ownPermissions(request bool) (ax bool, mic permissions.MicStatus, err error) {
	if os.Getenv(launchdEnv) == "1" {
		return permissions.Trusted(), permissions.Mic(), nil // already the daemon's context
	}
	exe, err := os.Executable()
	if err != nil {
		return false, 0, err
	}
	d, err := config.Dir()
	if err != nil {
		return false, 0, err
	}
	out := filepath.Join(d, "permcheck.out")
	plist := filepath.Join(d, "permcheck.plist")
	_ = os.Remove(out)

	args := []string{exe, "--perm-check", out}
	if request {
		args = append(args, "--perm-check-request")
	}
	if err := os.WriteFile(plist, permCheckPlist(args), 0o644); err != nil {
		return false, 0, err
	}
	target := launchdDomain() + "/" + permCheckLabel
	_ = exec.Command("launchctl", "bootout", target).Run()
	defer func() {
		_ = exec.Command("launchctl", "bootout", target).Run()
		_ = os.Remove(plist)
		_ = os.Remove(out)
	}()

	var lerr error
	var lout []byte
	for attempt := 0; attempt < 10; attempt++ {
		if lout, lerr = exec.Command("launchctl", "bootstrap", launchdDomain(), plist).CombinedOutput(); lerr == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lerr != nil {
		return false, 0, fmt.Errorf("launchctl bootstrap: %v: %s", lerr, strings.TrimSpace(string(lout)))
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
		ax, mic, ok := parsePermCheck(string(b))
		if ok {
			return ax, mic, nil
		}
	}
	return false, 0, fmt.Errorf("the permission check did not answer (is %s still in place?)", exe)
}

func permCheckPlist(args []string) []byte {
	esc := func(s string) string {
		var e bytes.Buffer
		_ = xml.EscapeText(&e, []byte(s))
		return e.String()
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	fmt.Fprintf(&b, "\t<key>Label</key><string>%s</string>\n", permCheckLabel)
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range args {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", esc(a))
	}
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key><true/>\n\t<key>KeepAlive</key><false/>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes()
}
