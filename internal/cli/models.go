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
	Short: "List speech models: what exists, what is downloaded, what is active",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		fmt.Printf("  %-3s %-20s %-30s %9s  %s\n", "", "NAME", "", "SIZE", "")
		for _, m := range catalog.Catalog {
			mark := "  "
			if m.Key == cfg.Model {
				mark = ok("●") + " "
			}
			state := dim("not downloaded")
			if m.Downloaded() {
				state = ok("downloaded")
			}
			rec := ""
			if m.Recommended {
				rec = blue(" recommended")
			}
			fmt.Printf("  %s %-20s %-30s %9s  %s%s\n", mark, m.Key, m.Label, catalog.Human(m.SizeBytes), state, rec)
			fmt.Printf("      %s\n", dim(m.Blurb))
		}
		fmt.Println()
		fmt.Println(dim("  ● active    download with: flowlite download <name>    switch with: flowlite use <name>"))
		return nil
	},
}

var downloadCmd = &cobra.Command{
	Use:   "download <name>",
	Short: "Download a model (resumes if interrupted)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, found := catalog.Get(args[0])
		if !found {
			return fmt.Errorf("unknown model %q — see `flowlite models`", args[0])
		}
		if m.Downloaded() {
			fmt.Printf("%s is already downloaded (%s)\n", m.Label, catalog.Human(m.DiskBytes()))
			return maybeActivate(m)
		}
		if err := downloadWithBar(m); err != nil {
			return err
		}
		return maybeActivate(m)
	},
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

// maybeActivate makes a freshly downloaded model the active one if nothing
// else is, so the common path needs no second command.
func maybeActivate(m catalog.Model) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Model == m.Key {
		return nil
	}
	if cur, has := catalog.Get(cfg.Model); has && cur.Downloaded() {
		fmt.Println(dim("  active model is still " + cur.Label + " — switch with: flowlite use " + m.Key))
		return nil
	}
	cfg.Model = m.Key
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("%s now the active model\n", ok("✓"))
	return nil
}

var removeCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Delete a downloaded model",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, found := catalog.Get(args[0])
		if !found {
			return fmt.Errorf("unknown model %q", args[0])
		}
		if !m.Downloaded() {
			fmt.Printf("%s is not downloaded\n", m.Label)
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
			fmt.Println(dim("  that was the active model — pick another with: flowlite use <name>"))
		}
		fmt.Printf("%s removed %s (freed %s)\n", ok("✓"), m.Label, freed)
		return nil
	},
}

var useCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Choose which downloaded model to dictate with",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, found := catalog.Get(args[0])
		if !found {
			return fmt.Errorf("unknown model %q — see `flowlite models`", args[0])
		}
		if !m.Downloaded() {
			return fmt.Errorf("%s is not downloaded yet — run: flowlite download %s", m.Label, m.Key)
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		cfg.Model = m.Key
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("%s active model: %s\n", ok("✓"), m.Label)
		if _, running := daemonRunning(); running {
			fmt.Println(dim("  restart the daemon to pick it up: flowlite stop && flowlite start"))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(modelsCmd, downloadCmd, removeCmd, useCmd)
}
