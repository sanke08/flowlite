package hotkey

import (
	"testing"
	"time"
)

const hold = 400 * time.Millisecond

// clock lets tests move time without sleeping.
type clock struct{ t time.Time }

func (c *clock) now() time.Time             { return c.t }
func (c *clock) advance(d time.Duration)    { c.t = c.t.Add(d) }
func newMachine() (*Machine, *clock) {
	c := &clock{t: time.Unix(0, 0)}
	m := New(hold)
	m.now = c.now
	return m, c
}

func TestHoldIsPushToTalk(t *testing.T) {
	m, c := newMachine()
	if got := m.Press(Target); got != Start {
		t.Fatalf("press = %v, want Start", got)
	}
	c.advance(hold + 50*time.Millisecond)
	if got := m.Release(Target); got != Finish {
		t.Fatalf("release = %v, want Finish", got)
	}
	if m.State() != Idle {
		t.Fatalf("state = %v, want Idle", m.State())
	}
}

func TestTapOpensAToggleSession(t *testing.T) {
	m, c := newMachine()
	m.Press(Target)
	c.advance(100 * time.Millisecond)
	if got := m.Release(Target); got != None {
		t.Fatalf("a quick tap must keep recording, got %v", got)
	}
	if m.State() != Toggle {
		t.Fatalf("state = %v, want Toggle", m.State())
	}
}

func TestSecondTapFinishes(t *testing.T) {
	m, c := newMachine()
	m.Press(Target)
	c.advance(100 * time.Millisecond)
	m.Release(Target)
	if got := m.Press(Target); got != Finish {
		t.Fatalf("second press = %v, want Finish", got)
	}
	if got := m.Release(Target); got != None {
		t.Fatalf("release after finish = %v, want None", got)
	}
	if m.State() != Idle {
		t.Fatalf("state = %v, want Idle", m.State())
	}
}

func TestEscapeCancelsAnOpenSession(t *testing.T) {
	m, c := newMachine()
	m.Press(Target)
	c.advance(100 * time.Millisecond)
	m.Release(Target)
	if got := m.Press(Escape); got != Cancel {
		t.Fatalf("esc = %v, want Cancel", got)
	}
	if m.State() != Idle {
		t.Fatalf("state = %v, want Idle", m.State())
	}
}

func TestKeyAutorepeatDoesNotRestart(t *testing.T) {
	m, c := newMachine()
	events := []Event{m.Press(Target), m.Press(Target), m.Press(Target)}
	c.advance(hold + 50*time.Millisecond)
	events = append(events, m.Release(Target))
	want := []Event{Start, None, None, Finish}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
}

func TestUnrelatedKeysAreIgnored(t *testing.T) {
	m, _ := newMachine()
	if m.Press(Other) != None || m.Release(Other) != None {
		t.Fatal("other keys must be ignored")
	}
	if m.State() != Idle {
		t.Fatal("state changed on an unrelated key")
	}
}

func TestEscapeWhileIdleIsInert(t *testing.T) {
	m, _ := newMachine()
	if got := m.Press(Escape); got != None {
		t.Fatalf("esc while idle = %v, want None", got)
	}
}

func TestReleaseWithoutPressIsIgnored(t *testing.T) {
	m, _ := newMachine()
	if got := m.Release(Target); got != None {
		t.Fatalf("stray release = %v, want None", got)
	}
}

func TestResetLetsTheNextGestureStartCleanly(t *testing.T) {
	// The daemon calls Reset when it stops recording on its own.
	m, c := newMachine()
	m.Press(Target)
	c.advance(100 * time.Millisecond)
	m.Release(Target) // toggle open
	m.Reset()
	if got := m.Press(Target); got != Start {
		t.Fatalf("press after reset = %v, want Start", got)
	}
	c.advance(hold + 50*time.Millisecond)
	if got := m.Release(Target); got != Finish {
		t.Fatalf("release after reset = %v, want Finish", got)
	}
}
