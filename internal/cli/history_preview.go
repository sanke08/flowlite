package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/sanke08/flowlite/internal/mainloop"
	"github.com/sanke08/flowlite/internal/overlay"
)

// historyPreview is `flowlite --history-preview <edge> --history-preview-out out.png`: open the
// transcript-history panel at that screen edge with a fixed set of fake
// entries, type query into its search field if one was given
// (--history-preview-query), snapshot it to out.png once the morph has
// settled, close it, snapshot the shrunk-back pill to out-closed.png, then show the
// plain pill once more and snapshot it to out-reshown.png.
// Whether the panel managed to take keyboard focus is printed so the
// search field's focus path can be checked without a hand on the keyboard. It exists so the panel's
// layout can be inspected without running the daemon; like --pill-preview it
// needs the AppKit main loop and so runs as its own process.
func historyPreview(pos, out, query string) error {
	if !overlay.ValidPosition(pos) {
		return fmt.Errorf("%q is not a position — choose one of: %s", pos, strings.Join(overlay.Positions, ", "))
	}
	overlay.SetPosition(pos)

	base := time.Date(2026, 1, 1, 3, 49, 0, 0, time.Local)
	texts := []string{
		"Hello there.",
		"So, yes, I think the approach we discussed yesterday is the right one, but we need to double-check the numbers before Friday.",
		"Remind me to call the dentist.",
		"The history panel should wrap long transcripts to at most three lines and then truncate, keeping the timestamp column aligned on the first line of every row so it always reads as one list.",
		"Okay.",
		"Let's implement the change in the overlay and verify it with a snapshot.",
		"Send the invoice to accounting and cc Priya.",
		"Short one.",
	}
	entries := make([]overlay.HistoryEntry, len(texts))
	for i, t := range texts {
		entries[i] = overlay.HistoryEntry{Time: base.Add(-time.Duration(i*7) * time.Minute), Text: t}
	}

	closed := make(chan struct{}, 1)
	var err error
	mainloop.Run(func() {
		defer mainloop.Stop()
		// Mirror the daemon: the pill has usually appeared and faded out
		// before the panel is asked for, so any state a fade leaves behind
		// (spring positions, alpha) is part of what the snapshot must show.
		overlay.Show(overlay.Listening, "")
		time.Sleep(400 * time.Millisecond)
		overlay.Hide()
		time.Sleep(700 * time.Millisecond)
		overlay.ShowHistory(entries, func(int) {}, func() { closed <- struct{}{} })
		time.Sleep(600 * time.Millisecond)
		fmt.Printf("history panel has key focus: %v\n", overlay.HistoryHasKey())
		if query != "" {
			overlay.SetHistoryQuery(query)
		}
		time.Sleep(600 * time.Millisecond)
		if out != "" {
			if err = overlay.SnapshotWindow(out); err != nil {
				return
			}
		}
		overlay.HideHistory()
		select {
		case <-closed:
		case <-time.After(2 * time.Second):
		}
		time.Sleep(200 * time.Millisecond)
		if out != "" {
			if err = overlay.SnapshotWindow(strings.TrimSuffix(out, ".png") + "-closed.png"); err != nil {
				return
			}
		}
		// A plain pill shown again after the panel has been open must be
		// the same PILL_LONG x PILL_SHORT capsule as before it — the
		// panel's layout must leave nothing behind (out-reshown.png).
		overlay.Show(overlay.Listening, "")
		overlay.SetLevel(0.6)
		time.Sleep(500 * time.Millisecond)
		if out != "" {
			err = overlay.SnapshotWindow(strings.TrimSuffix(out, ".png") + "-reshown.png")
		}
		overlay.Hide()
		time.Sleep(300 * time.Millisecond)
	})
	return err
}
