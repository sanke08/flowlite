// Package cli is the whole user interface. There are seven commands:
//
//	flowlite            start dictating (runs setup the first time)
//	flowlite settings   one menu for everything you can change
//	flowlite doctor     check what FlowLite needs and how to fix it
//	flowlite update     fetch the latest release
//	flowlite start      run in the background, detached from this terminal
//	flowlite stop       stop the background daemon
//	flowlite uninstall  remove FlowLite completely
package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/permissions"
)

// Set by the Makefile via -ldflags -X (see LDFLAGS there). The names are part
// of the build contract: internal/cli.Version, .Commit, .BuildDate,
// .WhisperVersion.
var (
	Version        = "dev"
	Commit         = "unknown"
	BuildDate      = "unknown"
	WhisperVersion = "unknown"
)

var (
	bold = lipgloss.NewStyle().Bold(true).Render
	dim  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render
	ok   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render
	warn = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render
	bad  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render
	blue = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render
)

// Hidden root flags: plumbing that used to be commands. They exist so that
// `settings` can spawn a subprocess for things that need the AppKit main
// loop (the pill) or the audio device, and so the background daemon has a
// re-exec target. Users never type them.
var (
	rootDaemon      bool   // background re-exec target: log to file, no banner
	rootPillPreview string // show the pill at this edge for ~3 s, then exit
	rootPlayCues    bool   // play the six audio cues, then exit
	rootNoPaste     bool   // user-facing: print transcripts instead of pasting
)

// errSilent is returned when everything worth saying has already been
// printed; Execute exits 1 without adding an "error:" line.
var errSilent = errors.New("")

var rootCmd = &cobra.Command{
	Use:   "flowlite",
	Short: "Press a key, speak, and the words appear where your cursor is — all on this machine.",
	Long: `FlowLite is local, free speech-to-text.

Run it and leave it running: hold the dictation key to talk, or double-tap it
to dictate hands-free and tap once to stop. Triple-tap to paste your last
transcript again. Nothing leaves your computer.

The first run walks you through setup.`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       Version,
	RunE:          runRoot,
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		if errors.Is(err, errSilent) {
			return 1
		}
		fmt.Fprintln(os.Stderr, bad("error:"), err)
		return 1
	}
	return 0
}

func loadConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("reading settings: %w", err)
	}
	return cfg, nil
}

// runRoot is bare `flowlite`: do the one thing standing between the user and
// dictating, then dictate. The branches are tried top to bottom and the
// first that fires decides.
func runRoot(cmd *cobra.Command, args []string) error {
	// Plumbing flags first; they are complete commands in themselves.
	if rootPillPreview != "" {
		return pillPreview(rootPillPreview)
	}
	if rootPlayCues {
		return playCues()
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && !rootDaemon

	// 1. Not on PATH: offer to put it there so `flowlite` works from any
	// terminal from now on.
	if interactive && !runningFromPath() {
		if err := offerInstall(); err != nil {
			return err
		}
	}

	// 2. Not configured: the setup wizard, then carry on without making the
	// user type anything again.
	if !configured(cfg) {
		if !interactive {
			return errors.New("not set up yet — run flowlite in a terminal once to choose a model")
		}
		if err := runSetup(cfg); err != nil {
			return err
		}
		fmt.Println()
		fmt.Println(dim("A few commands worth knowing:"))
		fmt.Println("  " + blue("flowlite settings") + dim("   change model, key, microphone, language, sounds…"))
		fmt.Println("  " + blue("flowlite doctor") + dim("     check what's needed and how to fix it"))
		fmt.Println("  " + blue("flowlite update") + dim("     fetch the latest release"))
		fmt.Println("  " + blue("flowlite start") + dim("      run in the background, detached from this terminal"))
		fmt.Println("  " + blue("flowlite uninstall") + dim("  remove FlowLite completely"))
		fmt.Println()
	}

	// 3. Keyboard permission missing: say exactly what to do and stop. A
	// daemon that cannot see the keyboard is only a support ticket.
	if permissions.Needed() && !permissions.Trusted() {
		fmt.Println(bad("Accessibility is NOT granted to " + hostApp() + "."))
		printAccessibilityFix(false)
		return errSilent
	}

	// 4. Already running: never start a second instance — two listeners
	// means two pastes per dictation.
	if pid, running := daemonRunning(); running {
		fmt.Printf("FlowLite is already listening (pid %d).\n", pid)
		fmt.Println(dim("  stop it with:  flowlite stop"))
		return nil
	}

	// 5. Dictate. FlowLite always runs in the background: closing the terminal
	// or pressing Ctrl+C in it must never take dictation away, because by then
	// it is part of how the user types. `flowlite stop` is the way to stop it.
	//
	// Two exceptions run here in the foreground instead. --daemon *is* the
	// background process, spawned by startBackground. --no-paste exists to
	// print transcripts instead of pasting them, which needs a terminal to
	// print to.
	if rootDaemon || rootNoPaste {
		return runDaemon(cfg, rootDaemon, rootNoPaste)
	}
	return startBackground()
}

// configured reports whether setup has finished: a model is chosen and its
// file is actually on disk (an interrupted download leaves the first true and
// the second false).
func configured(cfg *config.Config) bool {
	if !cfg.Configured() {
		return false
	}
	m, have := catalog.Get(cfg.Model)
	return have && m.Downloaded()
}

// offerInstall is step 1 of the root decision tree: copy the binary onto
// PATH if the user agrees. Declining is fine; nothing else depends on it.
func offerInstall() error {
	doInstall := true
	if err := huh.NewConfirm().
		Title("Install flowlite so it works from any terminal?").
		Description("Copies this file to " + installDir()).
		Affirmative("Yes").Negative("Skip").Value(&doInstall).Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil
		}
		return err
	}
	if !doInstall {
		return nil
	}
	dest, err := installSelf()
	if err != nil {
		fmt.Println(warn("  install skipped: " + err.Error()))
		return nil
	}
	fmt.Printf("%s installed to %s\n", ok("✓"), dest)
	if dir := dirOf(dest); !onPath(dir) {
		fmt.Println(dim("  add to ~/.zshrc, then open a new terminal:  export PATH=\"" + dir + ":$PATH\""))
	}
	fmt.Println()
	return nil
}

// printAccessibilityFix prints the numbered steps that get the keyboard
// permission granted. requested is true when the macOS prompt was just
// triggered, so step 1 reads as done.
func printAccessibilityFix(requested bool) {
	fmt.Println()
	fmt.Println("     Without it macOS never delivers the dictation key to FlowLite, so")
	fmt.Println("     pressing it does nothing at all. This is the step most people miss.")
	fmt.Println()
	if !requested {
		fmt.Println("       1.", blue("flowlite doctor --request"), dim("— opens the macOS prompt and adds "+hostApp()+" to the list"))
	} else {
		fmt.Println("       1.", dim("a macOS prompt should have appeared; it added "+hostApp()+" to the list"))
	}
	fmt.Println("       2. System Settings → Privacy & Security → Accessibility → switch on", bold(hostApp()))
	fmt.Println("       3. quit and reopen", bold(hostApp())+", then run:", blue("flowlite"), dim("(flowlite doctor confirms it)"))
	fmt.Println()
	fmt.Println(dim("     The permission attaches to " + hostApp() + " — the app that launched flowlite —"))
	fmt.Println(dim("     not to the flowlite binary, so it survives every update."))
}

// hostApp names the application macOS attributes our permissions to: the
// .app that (transitively) launched this process.
func hostApp() string {
	switch tp := os.Getenv("TERM_PROGRAM"); tp {
	case "Apple_Terminal":
		return "Terminal"
	case "iTerm.app":
		return "iTerm"
	case "vscode":
		return "Visual Studio Code"
	case "WarpTerminal":
		return "Warp"
	case "":
		if app := parentApp(); app != "" {
			return app
		}
		return "your terminal"
	default:
		return strings.TrimSuffix(tp, ".app")
	}
}

// parentApp walks up the process tree looking for a path inside an .app
// bundle and returns that bundle's name.
func parentApp() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	pid := os.Getppid()
	for depth := 0; depth < 12 && pid > 1; depth++ {
		out, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return ""
		}
		fields := strings.Fields(string(out))
		if len(fields) < 2 {
			return ""
		}
		comm := strings.Join(fields[1:], " ")
		if i := strings.Index(comm, ".app/"); i >= 0 {
			base := comm[:i]
			name := base[strings.LastIndex(base, "/")+1:]
			if name != "" {
				name = strings.ToUpper(name[:1]) + name[1:]
			}
			return name
		}
		next, err := strconv.Atoi(fields[0])
		if err != nil {
			return ""
		}
		pid = next
	}
	return ""
}

func shortenHome(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

func init() {
	rootCmd.SetVersionTemplate("flowlite {{.Version}}\n")
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	f := rootCmd.Flags()
	f.BoolVar(&rootNoPaste, "no-paste", false, "print transcripts here instead of pasting them (safe for trying it out)")
	f.BoolVar(&rootDaemon, "daemon", false, "internal: background daemon — log to file, no banner")
	f.StringVar(&rootPillPreview, "pill-preview", "", "internal: show the pill at this edge for a few seconds")
	f.BoolVar(&rootPlayCues, "play-cues", false, "internal: play the audio cues")
	_ = f.MarkHidden("daemon")
	_ = f.MarkHidden("pill-preview")
	_ = f.MarkHidden("play-cues")
}
