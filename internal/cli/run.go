package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/daemon"
	"github.com/sanke08/flowlite/internal/hotkey"
	"github.com/sanke08/flowlite/internal/mainloop"
	"github.com/sanke08/flowlite/internal/whisper"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Run FlowLite in the background, detached from this terminal",
	Args:  cobra.NoArgs,
	RunE:  func(cmd *cobra.Command, args []string) error { return startBackground() },
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background FlowLite daemon",
	Args:  cobra.NoArgs,
	RunE:  func(cmd *cobra.Command, args []string) error { return stopBackground() },
}

func init() {
	rootCmd.AddCommand(startCmd, stopCmd, reloadCmd)
}

// runDaemon loads the model, installs the hotkey and listens until it is told
// to stop. detached is the background mode spawned by startBackground — it
// logs to a file and prints nothing, and it is how FlowLite normally runs.
//
// The foreground path is reached only by `--no-paste`, which prints
// transcripts instead of pasting them and so needs a terminal to print to.
func runDaemon(cfg *config.Config, detached, noPaste bool) error {
	var logOut io.Writer = os.Stderr
	if detached {
		lp, _ := config.LogPath()
		f, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		logOut = f
	}
	logger := log.New(logOut, "", log.LstdFlags)

	logger.Printf("flowlite %s starting", Version)
	var spin *spinner
	if !detached {
		spin = startSpinner("loading the speech model…")
	}
	d, err := daemon.New(cfg, logger)
	spin.Stop()
	if err != nil {
		return err
	}
	d.NoPaste = noPaste
	if !detached {
		d.Transcribed = func(text string, secs, took float64) {
			fmt.Printf("  %s %s\n", dim(fmt.Sprintf("%4.1fs→%.2fs", secs, took)), text)
		}
	}

	if err := writePID(detached); err != nil {
		logger.Printf("pidfile: %v", err)
	}
	defer removePID()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// SIGHUP means "a setting changed, or the binary on disk did": finish
	// whatever is in flight, then replace this process with a fresh copy of
	// itself. The pid, the terminal and the permissions all survive, so the
	// change simply takes effect. Nothing is delivered here on Windows, where
	// the caller stops and starts the daemon instead.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		if _, open := <-hup; !open {
			return
		}
		logger.Printf("reloading")
		d.Close() // waits for a transcription in flight; releases the key tap
		removePID()
		// d.Close just tore down this pid's CoreAudio output device; exec-ing
		// immediately hands the (same pid, fresh image) process a new one
		// before the OS has necessarily finished releasing the old
		// registration, which is heard as a stuttering or missing first cue.
		// A cold-started daemon never hits this — there the old pid is a
		// separate, already-dying process, not this one an instant ago.
		// Nothing waits on this reload from the caller's side (reload(pid)
		// signals and returns immediately), so there is no cost to erring
		// generous rather than shaving this to the minimum that seemed to work.
		time.Sleep(500 * time.Millisecond)
		if err := reexecSelf(); err != nil {
			logger.Printf("reload failed: %v", err)
			stop() // could not reload, so shut down rather than run on stale
		}
	}()
	defer signal.Stop(hup)

	if !detached {
		printBanner(cfg, noPaste)
	}

	var runErr error
	mainloop.Run(func() {
		runErr = d.Run(ctx)
		mainloop.Stop()
	})

	if errors.Is(runErr, hotkey.ErrNotTrusted) {
		return fmt.Errorf("%w\n       Fix it with: flowlite doctor", runErr)
	}
	if runErr != nil {
		return runErr
	}
	logger.Printf("stopped")
	return nil
}

// printBanner is what the user sees while FlowLite listens: what is loaded,
// the gestures, where settings live, and — at most once a day — whether a
// newer release exists.
func printBanner(cfg *config.Config, noPaste bool) {
	model := cfg.Model
	if m, have := catalog.Get(cfg.Model); have {
		model = m.Label
	}
	engine := "CPU"
	if whisper.UsingMetal() {
		engine = "Metal"
	}
	key := hotkey.Label(cfg.Hotkey)
	fmt.Printf("%s   %s\n", bold("FlowLite "+Version+" is listening."), dim(model+" · "+key+" · "+engine))
	fmt.Printf("  hold %s to talk · double-tap for hands-free, tap to stop · triple-tap pastes your last transcript · Esc cancels\n", bold(key))
	fmt.Println(dim("  settings: flowlite settings (in another tab)"))
	if noPaste {
		fmt.Println(warn("  --no-paste: transcripts are printed here, not pasted"))
	}
	if notice := updateNotice(); notice != "" {
		fmt.Println("  " + notice)
	}
	fmt.Println()
}

// ---- background daemon -----------------------------------------------------

// startBackground launches `flowlite --daemon` detached from this terminal
// and waits for it to come up. It is reachable from settings → Background
// daemon; the foreground is the recommended way to run.
func startBackground() error {
	if pid, running := daemonRunning(); running {
		fmt.Printf("already running (pid %d)\n", pid)
		return nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if !configured(cfg) {
		return errors.New("no model installed yet — run: flowlite")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	lp, _ := config.LogPath()
	logf, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()

	c := exec.Command(exe, "--daemon")
	c.Stdout, c.Stderr = logf, logf
	detach(c)
	if err := c.Start(); err != nil {
		return err
	}
	// Give the model a moment to load so a failure shows up here. 15s: the
	// same budget stopBackground and waitForReload give — this is the same
	// daemon.New a cold start runs, which on a first run can block on the
	// macOS microphone-permission dialog and then a model load plus warm-up
	// (the README documents 15+ seconds for that alone).
	spin := startSpinner("starting…")
	defer spin.Stop()
	for i := 0; i < 150; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, running := daemonRunning(); running {
			spin.Stop()
			printStartedBanner(cfg, c.Process.Pid, lp)
			return nil
		}
	}
	return fmt.Errorf("the daemon did not come up; see %s", shortenHome(lp))
}

// stopBackground terminates whichever daemon the pidfile points at — the
// background one, or a foreground `flowlite` in another window.
func stopBackground() error {
	pid, running := daemonRunning()
	if !running {
		fmt.Println("not running")
		removePID()
		return nil
	}
	if err := terminate(pid); err != nil {
		return err
	}
	// Shutdown waits for any transcription in flight, so this has to allow
	// for a slow model finishing a long recording, not just process teardown.
	for i := 0; i < 150; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, still := daemonRunning(); !still {
			removePID()
			fmt.Printf("%s stopped (pid %d)\n", ok("✓"), pid)
			return nil
		}
	}
	return fmt.Errorf("pid %d did not exit", pid)
}

// writePID records the pid, and the mode alongside it in a separate file.
//
// The pidfile stays exactly "<pid>" — every version of FlowLite ever
// installed parses it, and one that cannot would decide nothing is running
// and start a second daemon. The mode goes in a sibling file that older
// binaries simply never look at.
func writePID(detached bool) error {
	p, err := config.PIDPath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return err
	}
	mode := modeForeground
	if detached {
		mode = modeDetached
	}
	if mp, err := config.ModePath(); err == nil {
		_ = os.WriteFile(mp, []byte(strconv.Itoa(os.Getpid())+" "+mode+"\n"), 0o644)
	}
	return nil
}

const (
	modeForeground = "foreground"
	modeDetached   = "detached"
)

// daemonIsForeground reports whether the running daemon is somebody's terminal
// session, which must not be killed to apply a setting. A missing or stale
// mode file reads as false: the behaviour before the file existed.
func daemonIsForeground() bool {
	pid, running := daemonRunning()
	if !running {
		return false
	}
	mp, err := config.ModePath()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(mp)
	if err != nil {
		return false
	}
	f := strings.Fields(strings.TrimSpace(string(b)))
	// The pid guards against a mode file left behind by an earlier daemon.
	return len(f) == 2 && f[0] == strconv.Itoa(pid) && f[1] == modeForeground
}

func removePID() {
	if p, err := config.PIDPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) == strconv.Itoa(os.Getpid()) {
			os.Remove(p)
			if mp, err := config.ModePath(); err == nil {
				os.Remove(mp)
			}
		} else if err == nil {
			// stale or someone else's — only remove if that pid is dead
			if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil && !alive(pid) {
				os.Remove(p)
			}
		}
	}
}

// daemonRunning reads the pidfile and checks the process is alive.
func daemonRunning() (int, bool) {
	p, err := config.PIDPath()
	if err != nil {
		return 0, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if !alive(pid) {
		return pid, false
	}
	return pid, true
}

// daemonStatus is the value shown on the settings row and in doctor.
func daemonStatus() string {
	pid, running := daemonRunning()
	if !running {
		return "not running"
	}
	// Which one it is changes what the user should do to it, so say so.
	if daemonIsForeground() {
		return fmt.Sprintf("running in a terminal (pid %d)", pid)
	}
	return fmt.Sprintf("running in the background (pid %d)", pid)
}

// applyToRunningDaemon makes a running daemon pick up new settings or a new
// binary. It reloads in place where it can, which keeps a foreground session
// alive; otherwise it falls back to a stop/start. Either way it waits for the
// daemon to actually come back and says so, rather than returning the moment
// the signal is sent — reload(pid) itself does not wait, so doing that would
// tell the user it's "applying" and hand the prompt straight back before the
// model, sounds and hotkey are actually live again, which is exactly the
// window where trying it looks like FlowLite broke.
func applyToRunningDaemon(what string) error {
	pid, running := daemonRunning()
	if !running {
		return nil
	}
	if err := reload(pid); err == nil {
		return waitForReload(what)
	}
	fmt.Println(dim("  restarting FlowLite to apply " + what + "…"))
	if err := stopBackground(); err != nil {
		return err
	}
	return startBackground()
}

// waitForReload watches the pidfile disappear — the old process tearing
// itself down — and come back with the same pid — the fresh image up,
// through daemon.New and writePID again — so "applied" means it actually
// is, not just that the signal made it there.
func waitForReload(what string) error {
	spin := startSpinner("applying " + what + "…")
	defer spin.Stop()

	sawGone := false
	// 15s: the same budget stopBackground already gives a shutdown: this is
	// the same daemon.New a cold start runs (model, sounds, hotkey), which is
	// exactly what that budget was sized for.
	for range 150 {
		time.Sleep(100 * time.Millisecond)
		pid, running := daemonRunning()
		if !running {
			sawGone = true
			continue
		}
		if sawGone {
			spin.Done(fmt.Sprintf("%s applied %s (pid %d)", ok("✓"), what, pid))
			return nil
		}
	}
	lp, _ := config.LogPath()
	if sawGone {
		return fmt.Errorf("FlowLite did not come back after applying %s — see %s", what, shortenHome(lp))
	}
	return fmt.Errorf("timed out waiting to apply %s — see %s", what, shortenHome(lp))
}

var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Make the running FlowLite pick up new settings or a new binary",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, running := daemonRunning(); !running {
			fmt.Println("not running")
			return nil
		}
		return applyToRunningDaemon("your changes")
	},
}

// printStartedBanner is what someone sees after `flowlite`. FlowLite runs in
// the background, so this is their one chance to be told the gestures and how
// to stop it — the terminal they typed in is free again immediately.
func printStartedBanner(cfg *config.Config, pid int, logPath string) {
	model := cfg.Model
	if m, have := catalog.Get(cfg.Model); have {
		model = m.Label
	}
	key := hotkey.Label(cfg.Hotkey)
	fmt.Printf("%s %s   %s\n", ok("✓"), bold("FlowLite "+Version+" is listening"), dim(model+" · "+key+" · pid "+strconv.Itoa(pid)))
	fmt.Printf("  hold %s to talk · double-tap for hands-free, tap to stop · triple-tap pastes your last transcript · Esc cancels\n", bold(key))
	fmt.Println(dim("  it keeps running when you close this window —  flowlite stop  ends it"))
	fmt.Println(dim("  settings: flowlite settings    log: " + shortenHome(logPath)))
	if notice := updateNotice(); notice != "" {
		fmt.Println("  " + notice)
	}
}
