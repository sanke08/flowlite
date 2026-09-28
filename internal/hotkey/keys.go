package hotkey

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// A dictation key is a chord: one key, or up to MaxChord keys held together,
// stored in config as their names joined by "+" (e.g. "alt_r", "fn",
// "fn+ctrl_l", "ctrl_r+space"). Modifiers come first in a fixed order, then
// the one ordinary key if there is one, so the same chord always has the same
// spelling however it was pressed.

// MaxChord is the most keys a dictation chord may have.
const MaxChord = 3

type keyKind int

const (
	kindModifier keyKind = iota // held with other keys; any order
	kindFunction                // F1–F20: do nothing much when pressed alone
	kindOrdinary                // letters, digits, space, arrows…: type or move
	kindReserved                // Esc cancels, Enter confirms; never part of a chord
)

type keyDef struct {
	name  string
	label string
	kind  keyKind
	mac   int    // Carbon virtual keycode (Events.h); -1 when absent
	win   uint32 // Windows virtual-key code; 0 when absent
}

// keyTable is every key FlowLite can recognise. Modifiers are listed in the
// order a chord spells them. macOS keycodes are positional (ANSI layout), so
// on another layout a letter's label names where the key sits, not what it
// types.
var keyTable = []keyDef{
	{"fn", "Fn", kindModifier, 63, 0}, // handled by the keyboard itself on Windows
	{"ctrl_l", "Left Control", kindModifier, 59, 0xA2},
	{"ctrl_r", "Right Control", kindModifier, 62, 0xA3},
	{"alt_l", "Left Option", kindModifier, 58, 0xA4},
	{"alt_r", "Right Option", kindModifier, 61, 0xA5},
	{"cmd_l", "Left Command", kindModifier, 55, 0x5B},
	{"cmd_r", "Right Command", kindModifier, 54, 0x5C},
	{"shift_l", "Left Shift", kindModifier, 56, 0xA0},
	{"shift_r", "Right Shift", kindModifier, 60, 0xA1},

	{"f1", "F1", kindFunction, 0x7A, 0x70},
	{"f2", "F2", kindFunction, 0x78, 0x71},
	{"f3", "F3", kindFunction, 0x63, 0x72},
	{"f4", "F4", kindFunction, 0x76, 0x73},
	{"f5", "F5", kindFunction, 0x60, 0x74},
	{"f6", "F6", kindFunction, 0x61, 0x75},
	{"f7", "F7", kindFunction, 0x62, 0x76},
	{"f8", "F8", kindFunction, 0x64, 0x77},
	{"f9", "F9", kindFunction, 0x65, 0x78},
	{"f10", "F10", kindFunction, 0x6D, 0x79},
	{"f11", "F11", kindFunction, 0x67, 0x7A},
	{"f12", "F12", kindFunction, 0x6F, 0x7B},
	{"f13", "F13", kindFunction, 0x69, 0x7C},
	{"f14", "F14", kindFunction, 0x6B, 0x7D},
	{"f15", "F15", kindFunction, 0x71, 0x7E},
	{"f16", "F16", kindFunction, 0x6A, 0x7F},
	{"f17", "F17", kindFunction, 0x40, 0x80},
	{"f18", "F18", kindFunction, 0x4F, 0x81},
	{"f19", "F19", kindFunction, 0x50, 0x82},
	{"f20", "F20", kindFunction, 0x5A, 0x83},

	{"a", "A", kindOrdinary, 0x00, 'A'},
	{"b", "B", kindOrdinary, 0x0B, 'B'},
	{"c", "C", kindOrdinary, 0x08, 'C'},
	{"d", "D", kindOrdinary, 0x02, 'D'},
	{"e", "E", kindOrdinary, 0x0E, 'E'},
	{"f", "F", kindOrdinary, 0x03, 'F'},
	{"g", "G", kindOrdinary, 0x05, 'G'},
	{"h", "H", kindOrdinary, 0x04, 'H'},
	{"i", "I", kindOrdinary, 0x22, 'I'},
	{"j", "J", kindOrdinary, 0x26, 'J'},
	{"k", "K", kindOrdinary, 0x28, 'K'},
	{"l", "L", kindOrdinary, 0x25, 'L'},
	{"m", "M", kindOrdinary, 0x2E, 'M'},
	{"n", "N", kindOrdinary, 0x2D, 'N'},
	{"o", "O", kindOrdinary, 0x1F, 'O'},
	{"p", "P", kindOrdinary, 0x23, 'P'},
	{"q", "Q", kindOrdinary, 0x0C, 'Q'},
	{"r", "R", kindOrdinary, 0x0F, 'R'},
	{"s", "S", kindOrdinary, 0x01, 'S'},
	{"t", "T", kindOrdinary, 0x11, 'T'},
	{"u", "U", kindOrdinary, 0x20, 'U'},
	{"v", "V", kindOrdinary, 0x09, 'V'},
	{"w", "W", kindOrdinary, 0x0D, 'W'},
	{"x", "X", kindOrdinary, 0x07, 'X'},
	{"y", "Y", kindOrdinary, 0x10, 'Y'},
	{"z", "Z", kindOrdinary, 0x06, 'Z'},
	{"0", "0", kindOrdinary, 0x1D, '0'},
	{"1", "1", kindOrdinary, 0x12, '1'},
	{"2", "2", kindOrdinary, 0x13, '2'},
	{"3", "3", kindOrdinary, 0x14, '3'},
	{"4", "4", kindOrdinary, 0x15, '4'},
	{"5", "5", kindOrdinary, 0x17, '5'},
	{"6", "6", kindOrdinary, 0x16, '6'},
	{"7", "7", kindOrdinary, 0x1A, '7'},
	{"8", "8", kindOrdinary, 0x1C, '8'},
	{"9", "9", kindOrdinary, 0x19, '9'},
	{"minus", "-", kindOrdinary, 0x1B, 0xBD},
	{"equal", "=", kindOrdinary, 0x18, 0xBB},
	{"bracket_l", "[", kindOrdinary, 0x21, 0xDB},
	{"bracket_r", "]", kindOrdinary, 0x1E, 0xDD},
	{"backslash", "\\", kindOrdinary, 0x2A, 0xDC},
	{"semicolon", ";", kindOrdinary, 0x29, 0xBA},
	{"quote", "'", kindOrdinary, 0x27, 0xDE},
	{"comma", ",", kindOrdinary, 0x2B, 0xBC},
	{"period", ".", kindOrdinary, 0x2F, 0xBE},
	{"slash", "/", kindOrdinary, 0x2C, 0xBF},
	{"grave", "`", kindOrdinary, 0x32, 0xC0},
	{"space", "Space", kindOrdinary, 0x31, 0x20},
	{"tab", "Tab", kindOrdinary, 0x30, 0x09},
	{"backspace", "Backspace", kindOrdinary, 0x33, 0x08},
	{"delete", "Delete", kindOrdinary, 0x75, 0x2E},
	{"home", "Home", kindOrdinary, 0x73, 0x24},
	{"end", "End", kindOrdinary, 0x77, 0x23},
	{"page_up", "Page Up", kindOrdinary, 0x74, 0x21},
	{"page_down", "Page Down", kindOrdinary, 0x79, 0x22},
	{"left", "Left Arrow", kindOrdinary, 0x7B, 0x25},
	{"right", "Right Arrow", kindOrdinary, 0x7C, 0x27},
	{"up", "Up Arrow", kindOrdinary, 0x7E, 0x26},
	{"down", "Down Arrow", kindOrdinary, 0x7D, 0x28},

	{"esc", "Esc", kindReserved, 53, 0x1B},
	{"enter", "Enter", kindReserved, 36, 0x0D},
	{"keypad_enter", "Enter", kindReserved, 0x4C, 0},
}

var byName = func() map[string]*keyDef {
	m := make(map[string]*keyDef, len(keyTable))
	for i := range keyTable {
		m[keyTable[i].name] = &keyTable[i]
	}
	return m
}()

// order is each key's position in keyTable, which is the order a chord's
// members are spelled in.
var order = func() map[string]int {
	m := make(map[string]int, len(keyTable))
	for i, k := range keyTable {
		m[k.name] = i
	}
	return m
}()

// lookup returns the key with this name if it exists on this platform.
func lookup(name string) (*keyDef, bool) {
	k, ok := byName[name]
	if !ok {
		return nil, false
	}
	if runtime.GOOS == "darwin" && k.mac < 0 || runtime.GOOS != "darwin" && k.win == 0 {
		return nil, false
	}
	return k, true
}

// keyLabel is the human name for one key, with the platform's own words.
func keyLabel(name string) string {
	k, ok := byName[name]
	if !ok {
		return name
	}
	if runtime.GOOS != "darwin" {
		switch name {
		case "alt_l":
			return "Left Alt"
		case "alt_r":
			return "Right Alt"
		case "cmd_l":
			return "Left Windows"
		case "cmd_r":
			return "Right Windows"
		}
	}
	return k.label
}

// Names lists the single keys offered when the keyboard cannot be listened
// to and the user has to pick from a list instead, in display order.
func Names() []string {
	if runtime.GOOS == "darwin" {
		return []string{"alt_r", "ctrl_r", "cmd_r", "shift_r", "fn", "f13", "f14", "f15"}
	}
	return []string{"ctrl_r", "alt_r", "shift_r", "f13", "f14", "f15"}
}

// Format spells a set of key names as a chord: deduplicated, modifiers first
// in a fixed order, joined by "+".
func Format(names []string) string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for i := 1; i < len(out); i++ { // tiny; insertion sort keeps it dependency-free
		for j := i; j > 0 && order[out[j]] < order[out[j-1]]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return strings.Join(out, "+")
}

// Parse splits a chord into its key names and checks it can be used as the
// dictation key on this platform.
func Parse(chord string) ([]string, error) {
	if strings.TrimSpace(chord) == "" {
		return nil, errors.New("no key")
	}
	names := strings.Split(strings.ToLower(strings.TrimSpace(chord)), "+")
	for i := range names {
		names[i] = strings.TrimSpace(names[i])
	}
	return names, Check(names)
}

// Check reports why a set of keys cannot be the dictation key, or nil.
func Check(names []string) error {
	if len(names) == 0 {
		return errors.New("no key")
	}
	if len(names) > MaxChord {
		return fmt.Errorf("at most %d keys at once", MaxChord)
	}
	seen := map[string]bool{}
	ordinary := 0
	var lone *keyDef
	for _, n := range names {
		k, ok := lookup(n)
		if !ok {
			return fmt.Errorf("%q is not a key FlowLite can listen for here", n)
		}
		if seen[n] {
			return fmt.Errorf("%s is listed twice", keyLabel(n))
		}
		seen[n] = true
		switch k.kind {
		case kindReserved:
			return fmt.Errorf("%s can't be part of the dictation key (Esc cancels, Enter confirms)", keyLabel(n))
		case kindOrdinary, kindFunction:
			ordinary++
		}
		lone = k
	}
	if ordinary > 1 {
		return errors.New("use at most one non-modifier key, together with modifiers")
	}
	if len(names) == 1 && lone.kind == kindOrdinary {
		return fmt.Errorf("%s on its own would fire every time you type it; hold a modifier with it", keyLabel(lone.name))
	}
	return nil
}

// Valid reports whether chord is a usable dictation key here.
func Valid(chord string) bool {
	_, err := Parse(chord)
	return err == nil
}

// Label is the human name for a chord, e.g. "Fn + Left Control".
func Label(chord string) string {
	names := strings.Split(chord, "+")
	labels := make([]string, len(names))
	for i, n := range names {
		labels[i] = keyLabel(n)
	}
	return strings.Join(labels, " + ")
}

// Contains reports whether the chord includes the named key.
func Contains(chord, name string) bool {
	for _, n := range strings.Split(chord, "+") {
		if n == name {
			return true
		}
	}
	return false
}

// Advice is a one-line caution about a chord that is valid but has a catch,
// or "" when there is nothing to say.
func Advice(chord string) string {
	names := strings.Split(chord, "+")
	if len(names) != 1 {
		return ""
	}
	switch names[0] {
	case "fn":
		return "macOS also acts on Fn/🌐 by itself — set System Settings → Keyboard → \"Press 🌐 key to\" → Do Nothing."
	case "ctrl_l", "alt_l", "cmd_l", "shift_l":
		return keyLabel(names[0]) + " is part of everyday shortcuts; holding it for one will start dictation too."
	}
	return ""
}

// DefaultName is Right Option on macOS and Right Control elsewhere: both sit
// under a resting hand and neither does anything when pressed alone.
func DefaultName() string {
	if runtime.GOOS == "darwin" {
		return "alt_r"
	}
	return "ctrl_r"
}

// isModifier reports whether the named key is a modifier.
func isModifier(name string) bool {
	k, ok := byName[name]
	return ok && k.kind == kindModifier
}

// nameForMac and nameForWin map a platform key code back to a key name.
var (
	nameForMac = func() map[int]string {
		m := map[int]string{}
		for _, k := range keyTable {
			if k.mac >= 0 {
				if _, dup := m[k.mac]; !dup {
					m[k.mac] = k.name
				}
			}
		}
		return m
	}()
	nameForWin = func() map[uint32]string {
		m := map[uint32]string{}
		for _, k := range keyTable {
			if k.win != 0 {
				if _, dup := m[k.win]; !dup {
					m[k.win] = k.name
				}
			}
		}
		return m
	}()
)
