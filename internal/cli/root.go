// Package cli is the whole user interface: every setting, every check and the
// daemon itself are reached from the `flowlite` command.
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/hotkey"
	"github.com/sanke08/flowlite/internal/permissions"
)

// Version is set by the Makefile from git describe.
var Version = "dev"

var (
	bold = lipgloss.NewStyle().Bold(true).Render
	dim  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render
	ok   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render
	warn = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render
	bad  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render
	blue = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render
)

var rootCmd = &cobra.Command{
	Use:   "flowlite",
	Short: "Press a key, speak, and the words appear where your cursor is — all on this machine.",
	Long: `FlowLite is local, free speech-to-text.

Tap the dictation key to start and stop, or hold it to dictate while pressed.
Nothing leaves your computer: the model runs here, and the only download is
the model itself.

Start with:  flowlite setup`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       Version,
	RunE:          runStatus,
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
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

// runStatus is `flowlite` with no arguments: where things stand, and the one
// thing to do next.
func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	// A freshly downloaded binary, double-clicked or run once: go straight
	// into setup instead of printing a status page nobody asked for.
	if !cfg.Configured() && term.IsTerminal(int(os.Stdin.Fd())) {
		return runSetup(cmd, args)
	}
	fmt.Println(bold("FlowLite"), dim(Version))
	fmt.Println()

	m, have := catalog.Get(cfg.Model)
	switch {
	case !have:
		fmt.Printf("  model      %s\n", warn("none chosen"))
	case !m.Downloaded():
		fmt.Printf("  model      %s %s\n", m.Label, warn("(not downloaded)"))
	default:
		fmt.Printf("  model      %s %s\n", m.Label, dim(catalog.Human(m.DiskBytes())))
	}
	fmt.Printf("  key        %s %s\n", hotkey.Label(cfg.Hotkey), dim("tap to toggle · hold to talk · Esc cancels"))

	trusted := permissions.Trusted()
	if permissions.Needed() {
		if trusted {
			fmt.Printf("  keyboard   %s %s\n", ok("allowed"), dim("Accessibility granted to "+hostApp()))
		} else {
			fmt.Printf("  keyboard   %s %s\n", bad("blocked"), dim("Accessibility not granted to "+hostApp()))
		}
	}

	if pid, running := daemonRunning(); running {
		fmt.Printf("  daemon     %s %s\n", ok("running"), dim(fmt.Sprintf("pid %d", pid)))
	} else {
		fmt.Printf("  daemon     %s\n", dim("not running"))
	}

	fmt.Println()
	switch {
	case !have || !m.Downloaded():
		fmt.Println("  next:", blue("flowlite setup"))
	case permissions.Needed() && !trusted:
		fmt.Println("  next:", blue("flowlite doctor"), dim("— the keyboard permission is the only thing missing"))
	default:
		fmt.Println("  next:", blue("flowlite run"), dim("— then tap "+hotkey.Label(cfg.Hotkey)+" in any text field"))
	}
	return nil
}

func init() {
	rootCmd.SetVersionTemplate("flowlite {{.Version}}\n")
}
