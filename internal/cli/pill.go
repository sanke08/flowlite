package cli

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/sanke08/flowlite/internal/mainloop"
	"github.com/sanke08/flowlite/internal/overlay"
)

// pillPreview is `flowlite --pill-preview <edge>`: show the pill at that
// screen edge for about three seconds — waveform, then the processing
// shimmer, then the success fade — and exit. settings → Pill position spawns
// it as a subprocess, because the pill needs the AppKit main loop and that
// can run only once per process.
func pillPreview(pos string) error {
	if !overlay.ValidPosition(pos) {
		return fmt.Errorf("%q is not a position — choose one of: %s", pos, strings.Join(overlay.Positions, ", "))
	}
	overlay.SetPosition(pos)
	mainloop.Run(func() {
		defer mainloop.Stop()

		// Recording: live waveform with a plausible speech envelope.
		overlay.Show(overlay.Listening, "")
		t0 := time.Now()
		for time.Since(t0) < 1600*time.Millisecond {
			t := time.Since(t0).Seconds()
			overlay.SetLevel(0.15 + 0.6*math.Abs(math.Sin(t*7))*(0.5+0.5*math.Sin(t*1.3)))
			time.Sleep(50 * time.Millisecond)
		}

		// Processing: settled bars with the shimmer sweep.
		overlay.SetState(overlay.Transcribing, "")
		time.Sleep(1000 * time.Millisecond)

		// Success: the pill fades out.
		overlay.SetState(overlay.Pasted, "")
		time.Sleep(600 * time.Millisecond)
		overlay.Hide()
		time.Sleep(200 * time.Millisecond)
	})
	return nil
}
