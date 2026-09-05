package hotkey

import (
	"testing"
	"time"
)

const (
	hold = 400 * time.Millisecond
	tap  = 100 * time.Millisecond // a quick press
	gap  = 150 * time.Millisecond // a pause between taps, inside the window
)

// clock lets tests move time without sleeping.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newMachine() (*Machine, *clock) {
	c := &clock{t: time.Unix(0, 0)}
	m := New(hold)
	m.now = c.now
	return m, c
}

// tapOnce presses and releases quickly, collecting every event including
// the Expire calls a 50 ms tick would produce.
func tapOnce(m *Machine, c *clock) []Event {
	var evs []Event
	evs = append(evs, m.Press(Target))
	evs = append(evs, tick(m, c, tap)...)
	evs = append(evs, m.Release(Target))
	return evs
}

// tick advances the clock in 50 ms steps, calling Expire like the daemon.
func tick(m *Machine, c *clock, d time.Duration) []Event {
	var evs []Event
	for elapsed := time.Duration(0); elapsed < d; elapsed += 50 * time.Millisecond {
		c.advance(50 * time.Millisecond)
		if e := m.Expire(); e != None {
			evs = append(evs, e)
		}
	}
	return evs
}

func expect(t *testing.T, what string, got Event, want Event) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

func expectState(t *testing.T, m *Machine, want Gesture) {
	t.Helper()
	if m.State() != want {
		t.Fatalf("state = %v, want %v", m.State(), want)
	}
}

func onlyEvents(evs []Event) []Event {
	var out []Event
	for _, e := range evs {
		if e != None {
			out = append(out, e)
		}
	}
	return out
}

func equal(a, b []Event) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestHoldIsPushToTalk(t *testing.T) {
	m, c := newMachine()
	expect(t, "press", m.Press(Target), Start)
	expectState(t, m, Undecided)
	got := tick(m, c, hold+50*time.Millisecond)
	if !equal(got, []Event{HoldConfirmed}) {
		t.Fatalf("ticks while holding = %v, want [hold-confirmed]", got)
	}
	expectState(t, m, Holding)
	expect(t, "release", m.Release(Target), Finish)
	expectState(t, m, Idle)
}

func TestHoldReleasedBeforeTickStillFinishes(t *testing.T) {
	// The daemon ticks every 50 ms; if the release lands first the machine
	// must not mistake a long press for a tap.
	m, c := newMachine()
	m.Press(Target)
	c.advance(hold + 10*time.Millisecond)
	expect(t, "release", m.Release(Target), Finish)
	expectState(t, m, Idle)
}

func TestSingleTapIsDiscardedWhenTheWindowCloses(t *testing.T) {
	m, c := newMachine()
	got := onlyEvents(tapOnce(m, c))
	if !equal(got, []Event{Start}) {
		t.Fatalf("tap = %v, want [start]", got)
	}
	expectState(t, m, Tapped)
	// Inside the window nothing happens.
	if evs := tick(m, c, 200*time.Millisecond); len(evs) != 0 {
		t.Fatalf("expire inside window = %v, want nothing", evs)
	}
	got = tick(m, c, 200*time.Millisecond)
	if !equal(got, []Event{Discard}) {
		t.Fatalf("expire after window = %v, want [discard]", got)
	}
	expectState(t, m, Idle)
}

// doubleTap performs tap, gap, tap and returns the machine in DoubleTapped
// with the window still open. Nothing but the first Start has fired.
func doubleTap(t *testing.T, m *Machine, c *clock) {
	t.Helper()
	got := onlyEvents(tapOnce(m, c))
	if !equal(got, []Event{Start}) {
		t.Fatalf("first tap = %v, want [start]", got)
	}
	if evs := tick(m, c, gap); len(evs) != 0 {
		t.Fatalf("expire between taps = %v, want nothing", evs)
	}
	expect(t, "second press", m.Press(Target), None)
	expectState(t, m, DoubleTapped)
	if evs := tick(m, c, tap); len(evs) != 0 {
		t.Fatalf("expire during second tap = %v, want nothing", evs)
	}
	expect(t, "second release", m.Release(Target), None)
	expectState(t, m, DoubleTapped)
}

func TestDoubleTapGoesHandsFree(t *testing.T) {
	m, c := newMachine()
	doubleTap(t, m, c)
	// The second tap alone confirms nothing: a third could still arrive.
	if evs := tick(m, c, 200*time.Millisecond); len(evs) != 0 {
		t.Fatalf("expire inside window = %v, want nothing", evs)
	}
	expectState(t, m, DoubleTapped)
	// Once the window closes, hands-free is confirmed exactly once.
	got := tick(m, c, 200*time.Millisecond)
	if !equal(got, []Event{StartHandsFree}) {
		t.Fatalf("expire after window = %v, want [start-hands-free]", got)
	}
	expectState(t, m, HandsFree)
	// Recording continues indefinitely with nothing held.
	if evs := tick(m, c, 5*time.Second); len(evs) != 0 {
		t.Fatalf("expire while hands-free = %v, want nothing", evs)
	}
	expectState(t, m, HandsFree)
}

func TestSecondTapHeldPastTheWindowWaitsForRelease(t *testing.T) {
	// A second press still held when the window would close is not yet a
	// gesture; hands-free is confirmed once the key is up and the window
	// closes after that release.
	m, c := newMachine()
	tapOnce(m, c)
	tick(m, c, gap)
	expect(t, "second press", m.Press(Target), None)
	if evs := tick(m, c, TapWindow+100*time.Millisecond); len(evs) != 0 {
		t.Fatalf("expire while second press held = %v, want nothing", evs)
	}
	expectState(t, m, DoubleTapped)
	expect(t, "second release", m.Release(Target), None)
	got := tick(m, c, TapWindow+100*time.Millisecond)
	if !equal(got, []Event{StartHandsFree}) {
		t.Fatalf("expire after release = %v, want [start-hands-free]", got)
	}
	expectState(t, m, HandsFree)
}

func TestOnePressStopsHandsFree(t *testing.T) {
	m, c := newMachine()
	doubleTap(t, m, c)
	got := tick(m, c, 2*time.Second)
	if !equal(got, []Event{StartHandsFree}) {
		t.Fatalf("expire after double tap = %v, want [start-hands-free]", got)
	}
	expectState(t, m, HandsFree)
	expect(t, "press while hands-free", m.Press(Target), Finish)
	expectState(t, m, Idle)
	expect(t, "release after finish", m.Release(Target), None)
	// However long that stopping press is held, nothing else fires.
	if evs := tick(m, c, time.Second); len(evs) != 0 {
		t.Fatalf("expire after finish = %v, want nothing", evs)
	}
}

func TestTripleTapPastesLast(t *testing.T) {
	m, c := newMachine()
	var evs []Event
	evs = append(evs, tapOnce(m, c)...)
	evs = append(evs, tick(m, c, gap)...)
	evs = append(evs, tapOnce(m, c)...)
	evs = append(evs, tick(m, c, gap)...)
	evs = append(evs, m.Press(Target))
	got := onlyEvents(evs)
	// No StartHandsFree on the way: the pill must never flash before paste.
	// And PasteLast itself does not fire on this press: the hotkey is a
	// modifier key, and pasting while it is still down would risk the
	// target app reading Cmd+V as Cmd+<modifier>+V.
	want := []Event{Start}
	if !equal(got, want) {
		t.Fatalf("triple tap = %v, want %v", got, want)
	}
	expectState(t, m, TripleTapped)
	// However long that third press is held, nothing fires — the paste
	// waits for the key to actually come up.
	if evs := tick(m, c, time.Second); len(evs) != 0 {
		t.Fatalf("expire while third press held = %v, want nothing", evs)
	}
	expect(t, "third release", m.Release(Target), PasteLast)
	expectState(t, m, Idle)
}

func TestEscapeWhileTripleTapHeldDiscards(t *testing.T) {
	// Esc pressed while the third tap is still down (waiting for release
	// before pasting): nothing was confirmed, so it's a discard, not a
	// cancel of a running recording.
	m, c := newMachine()
	tapOnce(m, c)
	tick(m, c, gap)
	tapOnce(m, c)
	tick(m, c, gap)
	m.Press(Target)
	expectState(t, m, TripleTapped)
	expect(t, "esc while third tap held", m.Press(Escape), Discard)
	expectState(t, m, Idle)
}

func TestEscapeDuringDoubleTapDiscards(t *testing.T) {
	// Two taps, Esc inside the window: nothing was confirmed to the user, so
	// the recording goes quietly.
	m, c := newMachine()
	doubleTap(t, m, c)
	tick(m, c, gap)
	expect(t, "esc while double-tapped", m.Press(Escape), Discard)
	expectState(t, m, Idle)
}

func TestPressAfterMissedExpireStopsHandsFree(t *testing.T) {
	// Should the daemon miss the tick that would confirm hands-free, a press
	// well after the window is the user stopping a recording they believe is
	// running, not a third tap.
	m, c := newMachine()
	doubleTap(t, m, c)
	c.advance(TapWindow + time.Second) // no Expire
	expect(t, "late press", m.Press(Target), Finish)
	expectState(t, m, Idle)
	expect(t, "release after finish", m.Release(Target), None)
}

func TestTapAfterTheWindowIsANewGesture(t *testing.T) {
	m, c := newMachine()
	tapOnce(m, c)
	got := tick(m, c, TapWindow+100*time.Millisecond)
	if !equal(got, []Event{Discard}) {
		t.Fatalf("expire = %v, want [discard]", got)
	}
	expect(t, "late press", m.Press(Target), Start)
	expectState(t, m, Undecided)
}

func TestLatePressWithoutExpireStartsFresh(t *testing.T) {
	// Should the daemon miss a tick, a press well after the window must
	// still start a new gesture rather than go hands-free.
	m, c := newMachine()
	tapOnce(m, c)
	c.advance(TapWindow + time.Second)
	expect(t, "late press", m.Press(Target), Start)
	expectState(t, m, Undecided)
}

func TestEscapeCancelsHandsFree(t *testing.T) {
	m, c := newMachine()
	doubleTap(t, m, c)
	got := tick(m, c, TapWindow+100*time.Millisecond)
	if !equal(got, []Event{StartHandsFree}) {
		t.Fatalf("expire after double tap = %v, want [start-hands-free]", got)
	}
	expect(t, "esc", m.Press(Escape), Cancel)
	expectState(t, m, Idle)
}

func TestEscapeCancelsAHold(t *testing.T) {
	m, c := newMachine()
	m.Press(Target)
	tick(m, c, hold+50*time.Millisecond)
	expect(t, "esc", m.Press(Escape), Cancel)
	expectState(t, m, Idle)
	// The key is still physically down; its release is not a gesture.
	expect(t, "release", m.Release(Target), None)
}

func TestEscapeBeforeConfirmationDiscards(t *testing.T) {
	m, c := newMachine()
	tapOnce(m, c)
	expect(t, "esc after one tap", m.Press(Escape), Discard)
	expectState(t, m, Idle)

	m.Press(Target)
	c.advance(100 * time.Millisecond)
	expect(t, "esc while undecided", m.Press(Escape), Discard)
	expectState(t, m, Idle)
}

func TestKeyAutorepeatDoesNotRestart(t *testing.T) {
	m, c := newMachine()
	events := []Event{m.Press(Target), m.Press(Target), m.Press(Target)}
	events = append(events, tick(m, c, hold+50*time.Millisecond)...)
	events = append(events, m.Press(Target)) // still repeating
	events = append(events, m.Release(Target))
	want := []Event{Start, None, None, HoldConfirmed, None, Finish}
	if !equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestUnrelatedKeysAreIgnored(t *testing.T) {
	m, _ := newMachine()
	if m.Press(Other) != None || m.Release(Other) != None {
		t.Fatal("other keys must be ignored")
	}
	expectState(t, m, Idle)
}

func TestEscapeWhileIdleIsInert(t *testing.T) {
	m, _ := newMachine()
	expect(t, "esc while idle", m.Press(Escape), None)
}

func TestReleaseWithoutPressIsIgnored(t *testing.T) {
	m, _ := newMachine()
	expect(t, "stray release", m.Release(Target), None)
}

func TestExpireWhileIdleIsInert(t *testing.T) {
	m, c := newMachine()
	if evs := tick(m, c, time.Second); len(evs) != 0 {
		t.Fatalf("expire while idle = %v, want nothing", evs)
	}
}

func TestResetLetsTheNextGestureStartCleanly(t *testing.T) {
	// The daemon calls Reset when it stops recording on its own.
	m, c := newMachine()
	doubleTap(t, m, c)
	tick(m, c, TapWindow+100*time.Millisecond) // hands-free open
	expectState(t, m, HandsFree)
	m.Reset()
	expectState(t, m, Idle)
	expect(t, "press after reset", m.Press(Target), Start)
	tick(m, c, hold+50*time.Millisecond)
	expect(t, "release after reset", m.Release(Target), Finish)
}

func TestResetWhileHeldForgetsTheKey(t *testing.T) {
	m, c := newMachine()
	m.Press(Target)
	tick(m, c, hold+50*time.Millisecond)
	m.Reset() // max duration reached mid-hold
	expect(t, "release after reset", m.Release(Target), None)
	expectState(t, m, Idle)
}
