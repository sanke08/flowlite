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
		// 65ms: a fast ticking ring, ~15 ticks a second. Each 30ms tick is
		// still separated by silence so it reads as ticks, not a tone; at
		// 50ms and below they fuse into one buzz.
		t := time.NewTicker(65 * time.Millisecond)
		defer t.Stop()
		// First tick lands just after the Stop cue finishes, not on top of it.
		select {
		case <-time.After(220 * time.Millisecond):
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
