package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/schollz/progressbar/v3"

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
)

// switchModel implements the one-model rule: fetch m if needed, make it
// active, and remove whatever else is on disk. The old model is deleted only
// once the new one is completely on disk, so an interrupted download never
// strands the user without a working model. Used by setup and by
// settings → Speech model.
func switchModel(cfg *config.Config, m catalog.Model) error {
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
	return nil
}

// downloadWithBar fetches m with a terminal progress bar. Ctrl+C leaves a
// resumable .part behind.
func downloadWithBar(m catalog.Model) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Printf("Downloading %s (%s) from Hugging Face…\n", m.Label, catalog.Human(m.SizeBytes))

	// The first progress callback fires once the response headers are in;
	// until then the connection setup is silent, so a spinner covers it. The
	// bar is created lazily inside that callback so its blank-state render
	// never interleaves with the spinner's line.
	spin := startSpinner("Connecting to Hugging Face…")
	defer spin.Stop()
	var bar *progressbar.ProgressBar
	err := catalog.Download(ctx, m, func(done, total int64) {
		if bar == nil {
			spin.Stop()
			bar = progressbar.NewOptions64(m.SizeBytes,
				progressbar.OptionSetDescription("  "+m.File),
				progressbar.OptionShowBytes(true),
				progressbar.OptionSetWidth(28),
				progressbar.OptionThrottle(65*time.Millisecond),
				progressbar.OptionClearOnFinish(),
				progressbar.OptionSetRenderBlankState(true),
			)
		}
		if total > 0 && bar.GetMax64() != total {
			bar.ChangeMax64(total)
		}
		_ = bar.Set64(done)
	})
	spin.Stop()
	if bar != nil {
		_ = bar.Finish()
		fmt.Println()
	}
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("interrupted — choose the same model again to resume")
		}
		return err
	}
	fmt.Printf("%s %s downloaded (%s)\n", ok("✓"), m.Label, catalog.Human(m.DiskBytes()))
	return nil
}
