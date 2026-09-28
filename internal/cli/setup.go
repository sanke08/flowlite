package cli

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/hotkey"
	"github.com/sanke08/flowlite/internal/permissions"
)

// runSetup is the first-run wizard, reached from bare `flowlite` when no
// model is on disk yet. It asks the three things that matter — permission,
// model, key — and saves. Whatever comes next (the permission steps, or
// dictating) is the root command's job, so nothing is retyped.
func runSetup(cfg *config.Config) error {
	fmt.Println(bold("FlowLite setup"))
	fmt.Println(dim("  Everything runs on this machine. The only download is the speech model."))
	fmt.Println()

	// 1. Keyboard permission — the step everyone misses, so it comes first.
	if permissions.Needed() && !permissions.Trusted() {
		fmt.Println(warn("macOS is not letting " + hostApp() + " see the keyboard yet."))
		fmt.Println(dim("  Without this the dictation key does nothing at all."))
		ask := true
		if err := runPrompt(huh.NewConfirm().
			Title("Open the macOS Accessibility prompt now?").
			Description("It adds " + hostApp() + " to Privacy & Security → Accessibility; you then switch it on.").
			Affirmative("Yes, open it").Negative("Later").Value(&ask)); err != nil {
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
	if err := runPrompt(huh.NewSelect[string]().
		Title("Speech model").
		Description("Turbo (compressed) is the best all-rounder: 99 languages, ~1.5 s per dictation.").
		Options(opts...).Value(&modelKey)); err != nil {
		return err
	}
	m, _ := catalog.Get(modelKey)
	if err := switchModel(cfg, m); err != nil {
		return err
	}

	// 3. Key.
	key, err := chooseKey(cfg.Hotkey)
	if errors.Is(err, huh.ErrUserAborted) {
		key, err = cfg.Hotkey, nil // Esc keeps the key already set
	}
	if err != nil {
		return err
	}
	cfg.Hotkey = key
	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("%s saved: %s, %s\n", ok("✓"), m.Label, hotkey.Label(key))
	printKeyAdvice(key)
	return nil
}
