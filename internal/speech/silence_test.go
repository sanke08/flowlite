package speech

import (
	"testing"
	"time"
)

func TestSilenceShouldStop(t *testing.T) {
	t0 := time.Unix(1000, 0)
	limit := 5500 * time.Millisecond
	var s Silence
	s.Reset(t0)

	// Quiet from the start: stops once the window has elapsed.
	if s.ShouldStop(t0.Add(5*time.Second), limit) {
		t.Fatal("stopped before the window elapsed")
	}
	if !s.ShouldStop(t0.Add(limit), limit) {
		t.Fatal("did not stop at the window")
	}

	// Speech resets the trailing window; room tone does not.
	s.Observe(0.1, t0.Add(4*time.Second))
	s.Observe(0.005, t0.Add(6*time.Second))
	if s.ShouldStop(t0.Add(9*time.Second), limit) {
		t.Fatal("stopped 5 s after speech")
	}
	if !s.ShouldStop(t0.Add(9500*time.Millisecond), limit) {
		t.Fatal("did not stop 5.5 s after speech")
	}

	// Zero limit disables auto-stop entirely.
	if s.ShouldStop(t0.Add(time.Hour), 0) {
		t.Fatal("limit 0 must disable auto-stop")
	}

	// A tracker that was never reset does not fire.
	var fresh Silence
	if fresh.ShouldStop(t0.Add(time.Hour), limit) {
		t.Fatal("unreset tracker fired")
	}
}
