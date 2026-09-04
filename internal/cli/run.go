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

	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/daemon"
	"github.com/sanke08/flowlite/internal/hotkey"
	"github.com/sanke08/flowlite/internal/mainloop"
	"github.com/sanke08/flowlite/internal/permissions"
)

var (
	runDetached bool
	runNoPaste  bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start dictating (foreground; Ctrl+C stops). The recommended way to run FlowLite.",
	Long: `Runs the dictation daemon in this terminal with a live log.

Running in the foreground has one big advantage on macOS: the Accessibility
permission attaches to your terminal app, so it is granted once and survives
every rebuild. Leave this running in a tab, or under tmux.`,
	RunE: runRun,
}

func runRun(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if !cfg.Configured() {
		return errors.New("no model chosen yet — run: flowlite setup")
	}
	if permissions.Needed() && !permissions.Trusted() {
		return fmt.Errorf("Accessibility is not granted to %s, so the dictation key can never reach FlowLite.\n       Fix it with: flowlite doctor", hostApp())
	}

	var logOut io.Writer = os.Stderr
	if runDetached {
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
	d, err := daemon.New(cfg, logger)
	if err != nil {
		return err
	}
	d.NoPaste = runNoPaste
	if !runDetached {
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

	if !runDetached {
		fmt.Println(bold("FlowLite is listening."))
		fmt.Printf("  tap %s to start and stop · hold it to dictate · Esc cancels · Ctrl+C quits\n",
			bold(hotkey.Label(cfg.Hotkey)))
		if runNoPaste {
			fmt.Println(warn("  --no-paste: transcripts are printed here, not pasted"))
		}
		fmt.Println()
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

// ---- background daemon -----------------------------------------------------

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start dictating in the background",
	RunE: func(cmd *cobra.Command, args []string) error {
		if pid, running := daemonRunning(); running {
			fmt.Printf("already running (pid %d)\n", pid)
			return nil
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if !cfg.Configured() {
			return errors.New("no model chosen yet — run: flowlite setup")
		}
		if permissions.Needed() && !permissions.Trusted() {
			return fmt.Errorf("Accessibility is not granted to %s — run: flowlite doctor", hostApp())
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

		c := exec.Command(exe, "run", "--daemon")
		c.Stdout, c.Stderr = logf, logf
		detach(c)
		if err := c.Start(); err != nil {
			return err
		}
		// Give the model a moment to load so a failure shows up here.
		for i := 0; i < 30; i++ {
			time.Sleep(100 * time.Millisecond)
			if _, running := daemonRunning(); running {
				fmt.Printf("%s running in the background (pid %d)\n", ok("✓"), c.Process.Pid)
				fmt.Println(dim("  log: " + shortenHome(lp) + "    stop: flowlite stop"))
				if permissions.Needed() {
					fmt.Println(dim("  if the key goes silent after a while, macOS may have dropped the"))
					fmt.Println(dim("  detached process's permission — use `flowlite run` in a tab instead."))
				}
				return nil
			}
		}
		return fmt.Errorf("the daemon did not come up; see %s", shortenHome(lp))
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
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
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Is the background daemon running?",
	RunE: func(cmd *cobra.Command, args []string) error {
		if pid, running := daemonRunning(); running {
			fmt.Printf("running (pid %d)\n", pid)
			return nil
		}
		fmt.Println("not running")
		return nil
	},
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

func init() {
	runCmd.Flags().BoolVar(&runDetached, "daemon", false, "internal: log to file instead of the terminal")
	runCmd.Flags().BoolVar(&runNoPaste, "no-paste", false, "print transcripts here instead of pasting them (safe for trying it out)")
	_ = runCmd.Flags().MarkHidden("daemon")
	rootCmd.AddCommand(runCmd, startCmd, stopCmd, statusCmd)
}
