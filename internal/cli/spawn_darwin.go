//go:build darwin

package cli

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sanke08/flowlite/internal/config"
)

// On macOS the background daemon is opened as a hidden app, FlowLite.app, by
// LaunchServices — the way `open -a Safari` starts Safari — not spawned by the
// shell or bootstrapped into launchd from it.
//
// Both of those leave the daemon credited to the terminal. setsid does not
// change it, and neither does a launchd job: launchd attributes a job to the
// coalition of whoever bootstrapped it (and onwards through any job that job
// bootstraps). Terminal quits through AppKit's sudden termination, and macOS
// then force-quits the app *and every service attributed to it*: the logs show
// "removing job: caller = loginwindow" for the daemon right after Terminal
// exits. An app opened by LaunchServices is its own app, attributed to no one,
// so nothing the user quits can take it down. That was checked with a test app
// that quits the same way Terminal does: a launchd job it bootstrapped was
// removed; an app it opened kept running.
//
// FlowLite.app lives in the config directory and is rebuilt on every start:
// an Info.plist (hidden, no Dock icon) and Contents/MacOS/flowlite, a symlink
// to this binary, so an update in place is picked up without touching it.
// macOS judges its permissions as the same flowlite as before — the
// Accessibility and microphone grants carry over.

const (
	appBundleName  = "FlowLite.app"
	appBundleID    = "com.sanke08.flowlite"
	legacyJobLabel = "com.sanke08.flowlite.daemon" // v0.6.2's launchd job
)

// spawnDaemon opens the daemon and returns without waiting for it. Shared by
// `flowlite start` (through spawnDaemonWatched) and by a daemon reloading
// itself.
func spawnDaemon() error {
	c, err := openDaemonCmd(false)
	if err != nil {
		return err
	}
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("open %s: %v: %s", appBundleName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// spawnDaemonWatched opens the daemon and reports, through exited, whether it
// has already quit — during startup that means it failed, and the log says
// why. stop releases the watcher; the daemon keeps running either way.
func spawnDaemonWatched() (exited func() bool, stop func(), err error) {
	c, err := openDaemonCmd(true) // open -W: returns when the app exits
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
	// Killing the `open -W` waiter does not touch the app it opened.
	stop = func() { _ = c.Process.Kill() }
	return exited, stop, nil
}

func openDaemonCmd(wait bool) (*exec.Cmd, error) {
	app, err := ensureAppBundle()
	if err != nil {
		return nil, err
	}
	lp, err := config.LogPath()
	if err != nil {
		return nil, err
	}
	// -n: a fresh instance even while the old one is still shutting down
	// (reload); -g: don't bring it forward; -j: launch it hidden.
	args := []string{"-n", "-g", "-j"}
	if wait {
		args = append(args, "-W")
	}
	args = append(args,
		"--env", backgroundEnv+"=1",
		"--stdout", lp, "--stderr", lp,
		app, "--args", "--daemon")
	c := exec.Command("open", args...)
	// open hands its environment to the app. Drop the v0.6.2 launchd marker
	// so a migrating daemon's successor does not try to migrate again.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, legacyLaunchdEnv+"=") {
			c.Env = append(c.Env, kv)
		}
	}
	return c, nil
}

// ensureAppBundle (re)writes FlowLite.app and returns its path.
func ensureAppBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	app := filepath.Join(d, appBundleName)
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		return "", err
	}

	plist := appInfoPlist()
	pp := filepath.Join(app, "Contents", "Info.plist")
	if cur, err := os.ReadFile(pp); err != nil || !bytes.Equal(cur, plist) {
		if err := os.WriteFile(pp, plist, 0o644); err != nil {
			return "", err
		}
	}

	link := filepath.Join(macos, "flowlite")
	if cur, err := os.Readlink(link); err != nil || cur != exe {
		_ = os.Remove(link)
		if err := os.Symlink(exe, link); err != nil {
			return "", err
		}
	}
	return app, nil
}

func appInfoPlist() []byte {
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
	kv := func(k, v string) { fmt.Fprintf(&b, "\t<key>%s</key><string>%s</string>\n", k, esc(v)) }
	kv("CFBundleIdentifier", appBundleID)
	kv("CFBundleName", "FlowLite")
	kv("CFBundleExecutable", "flowlite")
	kv("CFBundlePackageType", "APPL")
	kv("CFBundleInfoDictionaryVersion", "6.0")
	kv("CFBundleShortVersionString", strings.TrimPrefix(Version, "v"))
	kv("NSMicrophoneUsageDescription", "FlowLite listens while you hold the dictation key and turns your speech into text.")
	// A background agent: no Dock icon, no menu bar, never the active app.
	b.WriteString("\t<key>LSUIElement</key><true/>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes()
}

// removeDaemonJob deletes FlowLite.app and anything v0.6.2 left in launchd.
func removeDaemonJob() {
	removeLegacyJob()
	if d, err := config.Dir(); err == nil {
		_ = os.RemoveAll(filepath.Join(d, appBundleName))
		_ = os.Remove(filepath.Join(d, "daemon.plist"))
	}
}

// v0.6.2 ran the daemon as a launchd job, which Terminal's quit still took
// down. Unloading it is harmless when it is not there.
func removeLegacyJob() {
	_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+legacyJobLabel).Run()
}

// migrateLegacyDaemon is run by a daemon that finds itself started the v0.6.2
// way — as a launchd job, e.g. restarted by an old daemon's reload after an
// update. It opens FlowLite.app in its place and then unloads its own job,
// which ends this process. Returns an error only if the hand-over failed, in
// which case this daemon simply carries on as it is.
func migrateLegacyDaemon() error {
	if os.Getenv(legacyLaunchdEnv) != "1" {
		return errors.ErrUnsupported
	}
	if err := spawnDaemon(); err != nil {
		return err
	}
	removeLegacyJob() // SIGTERMs this process
	return nil
}
