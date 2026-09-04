package cli

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/sanke08/flowlite/internal/mainloop"
	"github.com/sanke08/flowlite/internal/overlay"
	"github.com/sanke08/flowlite/internal/sound"
)

var (
	pillSnapshotDir string
	pillNoSound     bool
)

// pill-demo walks the pill through every state, optionally saving PNGs of
// each. It exists to verify the UI without a microphone or a hotkey.
var pillDemoCmd = &cobra.Command{
	Use:    "pill-demo",
	Short:  "Show the on-screen pill in each state (for checking the UI)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if pillSnapshotDir != "" {
			if err := os.MkdirAll(pillSnapshotDir, 0o755); err != nil {
				return err
			}
		}
		player, _ := sound.NewPlayer(!pillNoSound)
		defer player.Close()

		snap := func(name string) {
			if pillSnapshotDir == "" {
				return
			}
			p := filepath.Join(pillSnapshotDir, name+".png")
			if err := overlay.Snapshot(p); err != nil {
				fmt.Fprintln(os.Stderr, "snapshot:", err)
			} else {
				fmt.Println("  saved", p)
			}
		}

		fmt.Println("Showing the pill at the bottom of your screen…")
		mainloop.Run(func() {
			defer mainloop.Stop()

			player.Play(sound.Start)
			overlay.Show(overlay.Listening, "")
			t0 := time.Now()
			for time.Since(t0) < 2600*time.Millisecond {
				t := time.Since(t0).Seconds()
				// A plausible speech envelope: syllable bursts.
				lvl := 0.15 + 0.6*math.Abs(math.Sin(t*7))*(0.5+0.5*math.Sin(t*1.3))
				overlay.SetLevel(lvl)
				if time.Since(t0) > 1400*time.Millisecond && time.Since(t0) < 1460*time.Millisecond {
					snap("1-listening")
				}
				time.Sleep(50 * time.Millisecond)
			}

			player.Play(sound.Stop)
			overlay.SetState(overlay.Transcribing, "Transcribing…")
			player.StartWorking()
			time.Sleep(900 * time.Millisecond)
			snap("2-transcribing")
			time.Sleep(1100 * time.Millisecond)
			player.StopWorking()

			overlay.SetState(overlay.Pasted, "Pasted")
			player.Play(sound.Done)
			time.Sleep(400 * time.Millisecond)
			snap("3-pasted")
			time.Sleep(600 * time.Millisecond)

			overlay.SetState(overlay.Cancelled, "Cancelled")
			player.Play(sound.Cancel)
			time.Sleep(400 * time.Millisecond)
			snap("4-cancelled")
			time.Sleep(400 * time.Millisecond)

			overlay.SetState(overlay.Error, "Paste failed")
			player.Play(sound.Error)
			time.Sleep(350 * time.Millisecond)
			snap("5-error")
			time.Sleep(700 * time.Millisecond)

			overlay.Hide()
			time.Sleep(450 * time.Millisecond) // let the fade-out finish
		})
		fmt.Println("done")
		return nil
	},
}

func init() {
	pillDemoCmd.Flags().StringVar(&pillSnapshotDir, "snapshot", "", "save a PNG of each state into this directory")
	pillDemoCmd.Flags().BoolVar(&pillNoSound, "silent", false, "do not play the cues")
	rootCmd.AddCommand(pillDemoCmd)
}
