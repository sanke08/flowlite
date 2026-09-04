package hotkey

import "runtime"

// Key names are the user-facing identifiers stored in config and accepted by
// `flowlite key`. They are keys that do nothing on their own and that almost
// no other software binds.
var names = map[string]string{
	"alt_r":   "Right Option",
	"ctrl_r":  "Right Control",
	"cmd_r":   "Right Command",
	"shift_r": "Right Shift",
	"f13":     "F13",
	"f14":     "F14",
	"f15":     "F15",
}

// Names lists the keys valid on this platform, in display order.
func Names() []string {
	if runtime.GOOS == "darwin" {
		return []string{"alt_r", "ctrl_r", "cmd_r", "shift_r", "f13", "f14", "f15"}
	}
	return []string{"ctrl_r", "alt_r", "shift_r", "f13", "f14", "f15"}
}

// Valid reports whether name is a usable key here.
func Valid(name string) bool {
	for _, n := range Names() {
		if n == name {
			return true
		}
	}
	return false
}

// Label is the human name for a key.
func Label(name string) string {
	if l, ok := names[name]; ok {
		if runtime.GOOS != "darwin" && name == "alt_r" {
			return "Right Alt"
		}
		return l
	}
	return name
}

// DefaultName is Right Option on macOS and Right Control elsewhere: both sit
// under a resting hand and neither does anything when pressed alone.
func DefaultName() string {
	if runtime.GOOS == "darwin" {
		return "alt_r"
	}
	return "ctrl_r"
}
