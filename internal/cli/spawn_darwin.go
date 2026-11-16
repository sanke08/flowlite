//go:build darwin

package cli

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sanke08/flowlite/internal/config"
)

// On macOS the background daemon is a launchd job, not a child of the shell.
//
// A daemon started from Terminal stays attributed to Terminal.app even after
// setsid (Terminal is its responsible app for TCC). Quitting Terminal (Cmd-Q,
// not just closing the window) was seen to stop it with a clean SIGTERM, most
// likely macOS tearing down the app's processes. A launchd job is not tied to
// any app, so nothing the user quits can take it down. The cost
// is that macOS then asks about Accessibility and the microphone for flowlite
// itself rather than for Terminal — a one-time grant.
//
// The job is bootstrapped into the user's GUI session from a plist kept in the
// config directory, not in ~/Library/LaunchAgents, so it does not start at
// login: it lives until `flowlite stop`, logout or a reboot, as before.

const launchdLabel = "com.sanke08.flowlite.daemon"

func launchdDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }
func launchdTarget() string { return launchdDomain() + "/" + launchdLabel }

// spawnDaemon starts `flowlite --daemon` in the background and returns
// without waiting for it. Shared by `flowlite start` and by a background
// daemon reloading itself.
func spawnDaemon() error {
	if os.Getenv(launchdEnv) == "1" {
		// Reload from inside the job: launchd stops this process (SIGTERM,
		// which runDaemon treats as a normal stop) and starts a fresh one
		// from the same definition. Start, not Run: launchctl may outlive
		// us, and AbandonProcessGroup keeps launchd from killing it.
		return exec.Command("launchctl", "kickstart", "-k", launchdTarget()).Start()
	}
	plist, err := writeLaunchdPlist()
	if err != nil {
		return err
	}
	// A job from an earlier start stays loaded (idle) after it exits; it has
	// to go before the label can be bootstrapped again. "Not loaded" is fine.
	_ = exec.Command("launchctl", "bootout", launchdTarget()).Run()

	// bootstrap straight after a bootout can fail with "5: Input/output
	// error" while launchd finishes removing the old job, so retry briefly.
	var out []byte
	for attempt := 0; attempt < 10; attempt++ {
		out, err = exec.Command("launchctl", "bootstrap", launchdDomain(), plist).CombinedOutput()
		if err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("launchctl bootstrap: %v: %s", err, strings.TrimSpace(string(out)))
}

// spawnedJobExited reports whether the launchd job has already run and exited,
// which during startup means the daemon failed (the reason is in the log).
func spawnedJobExited() bool {
	out, err := exec.Command("launchctl", "print", launchdTarget()).Output()
	if err != nil {
		return false
	}
	// The job's own state is the first tab-indented "state =" line; nested
	// sections have their own. A job that has not run yet is "spawn
	// scheduled"/"xpcproxy" and still says "(never exited)".
	s := string(out)
	return strings.Contains(s, "\n\tstate = not running") &&
		!strings.Contains(s, "(never exited)")
}

// removeDaemonJob unloads the launchd job and deletes its plist.
func removeDaemonJob() {
	_ = exec.Command("launchctl", "bootout", launchdTarget()).Run()
	if p, err := launchdPlistPath(); err == nil {
		_ = os.Remove(p)
	}
}

func launchdPlistPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "daemon.plist"), nil
}

func writeLaunchdPlist() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	lp, err := config.LogPath()
	if err != nil {
		return "", err
	}
	p, err := launchdPlistPath()
	if err != nil {
		return "", err
	}

	// launchd starts jobs with a minimal environment. Carry over what the
	// detached daemon used to inherit from the shell and could depend on.
	env := map[string]string{launchdEnv: "1"}
	for _, k := range []string{"PATH", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "TMPDIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}

	var b bytes.Buffer
	esc := func(s string) string {
		var e bytes.Buffer
		_ = xml.EscapeText(&e, []byte(s))
		return e.String()
	}
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	fmt.Fprintf(&b, "\t<key>Label</key><string>%s</string>\n", launchdLabel)
	fmt.Fprintf(&b, "\t<key>ProgramArguments</key>\n\t<array>\n\t\t<string>%s</string>\n\t\t<string>--daemon</string>\n\t</array>\n", esc(exe))
	b.WriteString("\t<key>RunAtLoad</key><true/>\n")
	// No restarts: `flowlite stop` must stay stopped.
	b.WriteString("\t<key>KeepAlive</key><false/>\n")
	// A reload's `launchctl kickstart` is a child of the daemon it restarts.
	b.WriteString("\t<key>AbandonProcessGroup</key><true/>\n")
	// launchd otherwise holds a restart for 10 s after the last spawn, which
	// two quick settings changes in a row would sit through.
	b.WriteString("\t<key>ThrottleInterval</key><integer>1</integer>\n")
	// Dictation is latency-sensitive; don't let it be throttled as background work.
	b.WriteString("\t<key>ProcessType</key><string>Interactive</string>\n")
	fmt.Fprintf(&b, "\t<key>StandardOutPath</key><string>%s</string>\n", esc(lp))
	fmt.Fprintf(&b, "\t<key>StandardErrorPath</key><string>%s</string>\n", esc(lp))
	b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
	for k, v := range env {
		fmt.Fprintf(&b, "\t\t<key>%s</key><string>%s</string>\n", esc(k), esc(v))
	}
	b.WriteString("\t</dict>\n</dict>\n</plist>\n")

	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		return "", err
	}
	return p, nil
}
