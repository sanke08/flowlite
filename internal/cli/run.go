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

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/daemon"
	"github.com/sanke08/flowlite/internal/hotkey"
	"github.com/sanke08/flowlite/internal/mainloop"
	"github.com/sanke08/flowlite/internal/whisper"
)

// runDaemon is branch 5 of bare `flowlite`: load the model, install the
// hotkey and listen until Ctrl+C. detached is the background mode spawned by
// startBackground — it logs to a file and prints nothing.
//
// Running in the foreground has one big advantage on macOS: the Accessibility
// permission attaches to the terminal app, so it is granted once and survives
// every update. A detached process can lose it.
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

	if err := writePID(); err != nil {
		logger.Printf("pidfile: %v", err)
	}
	defer removePID()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	fmt.Printf("  hold %s to talk · double-tap for hands-free, tap to stop · triple-tap pastes your last transcript · Esc cancels · Ctrl+C quits\n", bold(key))
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
	// Give the model a moment to load so a failure shows up here.
	spin := startSpinner("starting…")
	defer spin.Stop()
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, running := daemonRunning(); running {
			spin.Stop()
			fmt.Printf("%s running in the background (pid %d)\n", ok("✓"), c.Process.Pid)
			fmt.Println(dim("  log: " + shortenHome(lp) + "    stop it any time: flowlite settings → Background daemon → Stop"))
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
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, still := daemonRunning(); !still {
			removePID()
			fmt.Printf("%s stopped (pid %d)\n", ok("✓"), pid)
			return nil
		}
	}
	return fmt.Errorf("pid %d did not exit", pid)
}

func writePID() error {
	p, err := config.PIDPath()
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func removePID() {
	if p, err := config.PIDPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) == strconv.Itoa(os.Getpid()) {
			os.Remove(p)
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
	if pid, running := daemonRunning(); running {
		return fmt.Sprintf("running (pid %d)", pid)
	}
	return "not running"
}
