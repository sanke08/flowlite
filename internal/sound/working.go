package sound

import "time"

// The "still transcribing" tick, shared by both Player backends.

// StartWorking begins the periodic "still transcribing" tick.
func (p *Player) StartWorking() {
	if p == nil || !p.ready() {
		return
	}
	p.workMu.Lock()
	defer p.workMu.Unlock()
	if p.workStop != nil {
		return
	}
	stop := make(chan struct{})
	p.workStop = stop
	go func() {
		// 150ms: brisk enough to read as "working", but each 21ms tick is
		// still heard as its own event. 50ms (twenty ticks a second) fused
		// into one buzzing tone; 380ms felt sluggish next to a transcription
		// that usually finishes in well under a second.
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		// First tick lands just after the Stop cue finishes, not on top of it.
		select {
		case <-time.After(260 * time.Millisecond):
		case <-stop:
			return
		}
		for {
			p.Play(Working)
			select {
			case <-t.C:
			case <-stop:
				return
			}
		}
	}()
}

// StopWorking ends the tick.
func (p *Player) StopWorking() {
	if p == nil {
		return
	}
	p.workMu.Lock()
	if p.workStop != nil {
		close(p.workStop)
		p.workStop = nil
	}
	p.workMu.Unlock()
}
