package cli

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/mainloop"
	"github.com/sanke08/flowlite/internal/overlay"
	"github.com/sanke08/flowlite/internal/sound"
)

// pillPreview is `flowlite --pill-preview <edge>`: run one whole pretend
// dictation at that screen edge — the pill arrives with the Start cue, shows
// a waveform, drops into the processing shimmer with the Stop cue and the
// working ticker, then the Done cue and the fade — and exit. It is the same
// pill, cues and timings the daemon uses, so what you preview is what you
// get. settings → Pill position spawns it as a subprocess, because the pill
// needs the AppKit main loop and that can run only once per process.
func pillPreview(pos string) error {
	if !overlay.ValidPosition(pos) {
		return fmt.Errorf("%q is not a position — choose one of: %s", pos, strings.Join(overlay.Positions, ", "))
	}
	overlay.SetPosition(pos)

	// Cues follow the user's Sounds setting, like the daemon.
	sounds := true
	if cfg, err := config.Load(); err == nil {
		sounds = cfg.Sounds
	}
	player, _ := sound.NewPlayer(sounds) // a failed player is silent, never fatal
	defer player.Close()

	mainloop.Run(func() {
		defer mainloop.Stop()

		// Recording: live waveform with a plausible speech envelope.
		overlay.Show(overlay.Listening, "")
		player.Play(sound.Start)
		t0 := time.Now()
		for time.Since(t0) < 1800*time.Millisecond {
			t := time.Since(t0).Seconds()
			overlay.SetLevel(0.15 + 0.6*math.Abs(math.Sin(t*7))*(0.5+0.5*math.Sin(t*1.3)))
			time.Sleep(50 * time.Millisecond)
		}

		// Processing: settled bars with the shimmer sweep and the ticker.
		// Held a little longer than a real transcription usually takes so
		// the ticker's rhythm can actually be heard.
		overlay.SetState(overlay.Transcribing, "")
		player.Play(sound.Stop)
		player.StartWorking()
		time.Sleep(2200 * time.Millisecond)
		player.StopWorking()

		// Success: the Done cue and the pill fades out.
		overlay.SetState(overlay.Pasted, "")
		player.Play(sound.Done)
		time.Sleep(700 * time.Millisecond)
		overlay.Hide()
		time.Sleep(300 * time.Millisecond)
	})
	return nil
}
