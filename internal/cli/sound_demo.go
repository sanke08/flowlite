package cli

import (
	"fmt"
	"time"

	"github.com/sanke08/flowlite/internal/sound"
)

// playCues is `flowlite --play-cues`: play the six audio cues in the order a
// dictation would produce them, naming each, and exit. settings → Sounds
// spawns it so the user can hear what "on" means before deciding.
func playCues() error {
	p, err := sound.NewPlayer(true)
	if err != nil {
		return fmt.Errorf("opening audio output: %w", err)
	}
	defer p.Close()

	cues := []struct {
		cue  sound.Cue
		when string
	}{
		{sound.Start, "recording started"},
		{sound.Stop, "recording ended"},
		{sound.Working, "transcribing (repeats softly)"},
		{sound.Done, "text pasted"},
		{sound.Cancel, "cancelled, or nothing heard"},
		{sound.Error, "something failed"},
	}
	for _, c := range cues {
		fmt.Printf("  %-8s %s\n", c.cue, dim(c.when))
		p.Play(c.cue)
		n := len(sound.Samples(c.cue))
		time.Sleep(time.Duration(float64(n)/sound.Rate*float64(time.Second)) + 600*time.Millisecond)
	}
	return nil
}
