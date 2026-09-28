package hotkey

import (
	"runtime"
	"testing"
)

func TestOldKeysStillValid(t *testing.T) {
	for _, n := range Names() {
		if !Valid(n) {
			t.Errorf("%s: no longer valid", n)
		}
	}
	if !Valid(DefaultName()) {
		t.Errorf("default %s not valid", DefaultName())
	}
}

func TestCheck(t *testing.T) {
	good := []string{"alt_r", "f13", "f5", "ctrl_l+space", "ctrl_r+shift_r", "cmd_l+alt_l+k"}
	bad := []string{"", "a", "space", "esc", "enter", "ctrl_l+enter", "ctrl_l+a+b",
		"ctrl_l+ctrl_l", "ctrl_l+alt_l+cmd_l+shift_l", "nope", "f13+f14"}
	if runtime.GOOS == "darwin" {
		good = append(good, "fn", "fn+ctrl_l")
	} else {
		bad = append(bad, "fn")
	}
	for _, c := range good {
		if _, err := Parse(c); err != nil {
			t.Errorf("%q: unexpected error %v", c, err)
		}
	}
	for _, c := range bad {
		if _, err := Parse(c); err == nil {
			t.Errorf("%q: accepted, want rejected", c)
		}
	}
}

func TestFormatOrdersModifiersFirst(t *testing.T) {
	if got := Format([]string{"space", "shift_r", "ctrl_l", "space"}); got != "ctrl_l+shift_r+space" {
		t.Errorf("Format = %q", got)
	}
}

func TestKeycodesUnique(t *testing.T) {
	mac, win := map[int]string{}, map[uint32]string{}
	for _, k := range keyTable {
		if k.name == "keypad_enter" {
			continue
		}
		if p, dup := mac[k.mac]; dup && k.mac >= 0 {
			t.Errorf("mac keycode %d used by %s and %s", k.mac, p, k.name)
		}
		mac[k.mac] = k.name
		if p, dup := win[k.win]; dup && k.win != 0 {
			t.Errorf("windows vk %#x used by %s and %s", k.win, p, k.name)
		}
		win[k.win] = k.name
	}
}

type feedStep struct {
	name string
	down bool
	want string // "", "down", "up", "esc"
}

func runChord(t *testing.T, spec string, steps []feedStep) *chord {
	t.Helper()
	c := newChord(spec)
	for i, s := range steps {
		ev, ok := c.feed(s.name, s.down)
		got := ""
		if ok {
			switch {
			case ev.Kind == Escape:
				got = "esc"
			case ev.Down:
				got = "down"
			default:
				got = "up"
			}
		}
		if got != s.want {
			t.Fatalf("%s step %d (%s down=%v): got %q, want %q", spec, i, s.name, s.down, got, s.want)
		}
	}
	return c
}

func TestChordSingleKey(t *testing.T) {
	runChord(t, "alt_r", []feedStep{
		{"alt_r", true, "down"},
		{"alt_r", true, ""}, // auto-repeat
		{"a", true, ""},
		{"alt_r", false, "up"},
		{"alt_r", false, ""},
		{"esc", true, "esc"},
	})
}

func TestChordModifiersAnyOrder(t *testing.T) {
	c := runChord(t, "fn+ctrl_l", []feedStep{
		{"ctrl_l", true, ""},
		{"fn", true, "down"},
		{"fn", false, "up"},
		{"fn", true, "down"}, // re-press while the other is still held
		{"ctrl_l", false, "up"},
	})
	if c.allUp() {
		t.Error("allUp with fn still held")
	}
	c.feed("fn", false)
	if !c.allUp() {
		t.Error("not allUp after everything released")
	}
}

func TestChordOrdinaryKeyMustComeLast(t *testing.T) {
	runChord(t, "ctrl_r+space", []feedStep{
		{"space", true, ""},  // typing a space…
		{"ctrl_r", true, ""}, // …then touching Control: not the shortcut
		{"space", false, ""},
		{"space", true, "down"}, // a fresh space with Control held is
		{"space", false, "up"},
		{"space", true, "down"}, // and can be tapped again
		{"ctrl_r", false, "up"},
	})
}

func TestRightShiftTracked(t *testing.T) {
	c := newChord("alt_r")
	c.feed("shift_r", true)
	if !c.rightShift() {
		t.Error("right shift not tracked")
	}
	c.feed("shift_r", false)
	if c.rightShift() {
		t.Error("right shift stuck")
	}
}

func TestRecorder(t *testing.T) {
	var r Recorder
	r.Feed("enter", false) // the Enter that opened the prompt coming up
	if r.Combo() != "" {
		t.Fatalf("combo %q from a reserved key", r.Combo())
	}
	r.Feed("ctrl_l", true)
	r.Feed("space", true)
	if r.Held() != "ctrl_l+space" {
		t.Fatalf("held %q", r.Held())
	}
	r.Feed("space", false)
	r.Feed("ctrl_l", false)
	if r.Held() != "" || r.Combo() != "ctrl_l+space" {
		t.Fatalf("after release: held %q combo %q", r.Held(), r.Combo())
	}
	// Releasing part of the chord keeps the bigger set.
	r.Feed("shift_r", true)
	if r.Combo() != "shift_r" {
		t.Fatalf("fresh press should replace: %q", r.Combo())
	}
	r.Feed("f13", true)
	r.Feed("f13", false)
	r.Feed("shift_r", false)
	if r.Combo() != "shift_r+f13" {
		t.Fatalf("combo %q", r.Combo())
	}
	// A key already down before listening started is ignored on release.
	var r2 Recorder
	if r2.Feed("alt_r", false) {
		t.Error("release of an unseen key reported a change")
	}
}
