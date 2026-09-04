package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"

	"github.com/sanke08/flowlite/internal/catalog"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List speech models; ● marks the one installed",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		for _, m := range catalog.Catalog {
			mark := "  "
			if m.Key == cfg.Model && m.Downloaded() {
				mark = ok("●") + " "
			}
			state := dim("not installed")
			if m.Downloaded() {
				state = ok("installed")
			}
			rec := ""
			if m.Recommended {
				rec = blue(" recommended")
			}
			fmt.Printf("  %s %-20s %-30s %9s  %s%s\n", mark, m.Key, m.Label, catalog.Human(m.SizeBytes), state, rec)
			fmt.Printf("      %s\n", dim(m.Blurb))
		}
		fmt.Println()
		fmt.Println(dim("  FlowLite keeps one model on disk. Switch with: flowlite use <name>"))
		extraModelsWarning()
		return nil
	},
}

// useCmd is the single model operation: fetch if needed, make it active, and
// remove whatever else is on disk. `download` is kept as an alias.
var useCmd = &cobra.Command{
	Use:     "use <name>",
	Aliases: []string{"download"},
	Short:   "Switch to a model: download it if needed, then remove the previous one",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, found := catalog.Get(args[0])
		if !found {
			return fmt.Errorf("unknown model %q — see `flowlite models`", args[0])
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return switchModel(cfg, m)
	},
}

// switchModel implements the one-model rule. The old model is deleted only
// once the new one is completely on disk, so an interrupted download never
// strands the user without a working model.
func switchModel(cfg *configT, m catalog.Model) error {
	if !m.Downloaded() {
		if cur, has := catalog.Get(cfg.Model); has && cur.Downloaded() && cur.Key != m.Key {
			fmt.Println(dim("  " + cur.Label + " will be removed once " + m.Label + " has finished downloading."))
		}
		if err := downloadWithBar(m); err != nil {
			return err
		}
	}
	cfg.Model = m.Key
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("%s active model: %s\n", ok("✓"), m.Label)

	removed, err := catalog.PruneExcept(m.Key)
	for _, r := range removed {
		fmt.Printf("%s removed %s (freed %s)\n", ok("✓"), r.Model.Label, catalog.Human(r.Bytes))
	}
	if err != nil {
		fmt.Println(warn("  could not remove an old model: " + err.Error()))
	}
	if _, running := daemonRunning(); running {
		fmt.Println(dim("  restart the daemon to pick it up: flowlite stop && flowlite start"))
	}
	return nil
}

// downloadWithBar fetches m with a terminal progress bar. Ctrl+C leaves a
// resumable .part behind.
func downloadWithBar(m catalog.Model) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Printf("Downloading %s (%s) from Hugging Face…\n", m.Label, catalog.Human(m.SizeBytes))
	bar := progressbar.NewOptions64(m.SizeBytes,
		progressbar.OptionSetDescription("  "+m.File),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(28),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetRenderBlankState(true),
	)
	err := catalog.Download(ctx, m, func(done, total int64) {
		if total > 0 && bar.GetMax64() != total {
			bar.ChangeMax64(total)
		}
		_ = bar.Set64(done)
	})
	_ = bar.Finish()
	fmt.Println()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("interrupted — run the same command again to resume")
		}
		return err
	}
	fmt.Printf("%s %s downloaded (%s)\n", ok("✓"), m.Label, catalog.Human(m.DiskBytes()))
	return nil
}

var removeCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Delete an installed model",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, found := catalog.Get(args[0])
		if !found {
			return fmt.Errorf("unknown model %q", args[0])
		}
		if !m.Downloaded() {
			fmt.Printf("%s is not installed\n", m.Label)
			return nil
		}
		freed := catalog.Human(m.DiskBytes())
		if err := m.Remove(); err != nil {
			return err
		}
		cfg, err := loadConfig()
		if err == nil && cfg.Model == m.Key {
			cfg.Model = ""
			_ = cfg.Save()
			fmt.Println(dim("  that was the active model — choose another with: flowlite use <name>"))
		}
		fmt.Printf("%s removed %s (freed %s)\n", ok("✓"), m.Label, freed)
		return nil
	},
}

// extraModelsWarning nudges when the one-model rule is violated (files that
// predate it, or an interrupted switch).
func extraModelsWarning() {
	if inst := catalog.Installed(); len(inst) > 1 {
		var total int64
		for _, m := range inst {
			total += m.DiskBytes()
		}
		fmt.Println(warn(fmt.Sprintf("  %d models on disk (%s). FlowLite keeps one — run: flowlite use <name>", len(inst), catalog.Human(total))))
	}
}

func init() {
	rootCmd.AddCommand(modelsCmd, useCmd, removeCmd)
}
