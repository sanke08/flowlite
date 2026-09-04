package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/hotkey"
	"github.com/sanke08/flowlite/internal/permissions"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive first-run: permission, model download, dictation key",
	RunE:  runSetup,
}

func runSetup(cmd *cobra.Command, args []string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("setup is interactive — in scripts use: flowlite download <model>, flowlite use <model>, flowlite key <name>")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	fmt.Println(bold("FlowLite setup"))
	fmt.Println(dim("  Everything runs on this machine. The only download is the speech model."))
	fmt.Println()

	// 1. Keyboard permission — the step everyone misses, so it comes first.
	if permissions.Needed() && !permissions.Trusted() {
		fmt.Println(warn("macOS is not letting " + hostApp() + " see the keyboard yet."))
		fmt.Println(dim("  Without this the dictation key does nothing at all."))
		var ask bool = true
		if err := huh.NewConfirm().
			Title("Open the macOS Accessibility prompt now?").
			Description("It adds " + hostApp() + " to Privacy & Security → Accessibility; you then switch it on.").
			Affirmative("Yes, open it").Negative("Later").Value(&ask).Run(); err != nil {
			return err
		}
		if ask {
			permissions.Request()
			fmt.Println(dim("  → switch on " + hostApp() + " in the window that opened, then come back here."))
			fmt.Println()
		}
	}

	// 2. Model.
	opts := make([]huh.Option[string], 0, len(catalog.Catalog))
	for _, m := range catalog.Catalog {
		label := fmt.Sprintf("%-30s %8s", m.Label, catalog.Human(m.SizeBytes))
		switch {
		case m.Downloaded():
			label += "   downloaded"
		case m.Recommended:
			label += "   recommended"
		}
		opts = append(opts, huh.NewOption(label, m.Key))
	}
	modelKey := cfg.Model
	if modelKey == "" {
		modelKey = catalog.Default().Key
	}
	if err := huh.NewSelect[string]().
		Title("Speech model").
		Description("Turbo (compressed) is the best all-rounder: 99 languages, ~1.5 s per dictation.").
		Options(opts...).Value(&modelKey).Run(); err != nil {
		return err
	}
	m, _ := catalog.Get(modelKey)
	if err := switchModel(cfg, m); err != nil {
		return err
	}

	// 3. Key.
	keyOpts := make([]huh.Option[string], 0)
	for _, n := range hotkey.Names() {
		keyOpts = append(keyOpts, huh.NewOption(fmt.Sprintf("%-16s %s", hotkey.Label(n), dim(n)), n))
	}
	key := cfg.Hotkey
	if err := huh.NewSelect[string]().
		Title("Dictation key").
		Description("Tap to start and stop, hold to dictate while pressed, Esc to cancel.").
		Options(keyOpts...).Value(&key).Run(); err != nil {
		return err
	}
	cfg.Hotkey = key

	if err := cfg.Save(); err != nil {
		return err
	}

	// 4. Put the binary somewhere `flowlite` resolves from any terminal.
	if !runningFromPath() {
		doInstall := true
		if err := huh.NewConfirm().
			Title("Install flowlite so it works from any terminal?").
			Description("Copies this file to " + installDir()).
			Affirmative("Yes").Negative("Skip").Value(&doInstall).Run(); err != nil {
			return err
		}
		if doInstall {
			if dest, err := installSelf(); err != nil {
				fmt.Println(warn("  install skipped: " + err.Error()))
			} else {
				fmt.Printf("%s installed to %s\n", ok("✓"), dest)
				if !onPath(filepath.Dir(dest)) {
					fmt.Println(dim("  add to ~/.zshrc:  export PATH=\"" + filepath.Dir(dest) + ":$PATH\""))
				}
			}
		}
	}

	fmt.Println()
	fmt.Printf("%s saved: %s, %s\n", ok("✓"), m.Label, hotkey.Label(key))
	fmt.Println()
	if permissions.Needed() && !permissions.Trusted() {
		fmt.Println("Next:")
		fmt.Println("  1. System Settings → Privacy & Security → Accessibility → switch on", bold(hostApp()))
		fmt.Println("  2.", blue("flowlite doctor"), dim("to confirm"))
		fmt.Println("  3.", blue("flowlite run"))
	} else {
		fmt.Println("Next:", blue("flowlite run"), dim("— then tap "+hotkey.Label(key)+" in any text field"))
		fmt.Println(dim("      or try the pipeline alone first: flowlite test"))
	}
	return nil
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
