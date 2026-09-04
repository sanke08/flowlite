// Package inject puts text into whatever field currently has focus.
//
// Pasting is used rather than synthesising one key event per character: it is
// near-instant regardless of length, does not mangle non-ASCII text, and does
// not trigger autocomplete on every keystroke.
package inject

import "time"

const (
	// Give the OS a moment to register the new clipboard owner before the
	// paste; without this, fast machines occasionally paste the old contents.
	preDelay = 50 * time.Millisecond
	// The target app reads the clipboard when it handles the paste, which is
	// asynchronous from our point of view.
	restoreDelay = 350 * time.Millisecond
)

// Paste puts text on the clipboard, sends the paste shortcut, and (optionally)
// restores what was there before.
func Paste(text string, restore bool) error {
	if text == "" {
		return nil
	}
	var previous string
	var hadPrevious bool
	if restore {
		previous, hadPrevious = clipboardGet()
	}
	if err := clipboardSet(text); err != nil {
		return err
	}
	time.Sleep(preDelay)
	if err := pasteKeystroke(); err != nil {
		return err
	}
	if restore && hadPrevious {
		time.Sleep(restoreDelay)
		_ = clipboardSet(previous)
	}
	return nil
}

// ClipboardRoundTrip is used by `doctor`: write, read back, restore.
func ClipboardRoundTrip() error {
	prev, had := clipboardGet()
	if err := clipboardSet("flowlite-self-test"); err != nil {
		return err
	}
	got, _ := clipboardGet()
	if had {
		_ = clipboardSet(prev)
	} else {
		_ = clipboardSet("")
	}
	if got != "flowlite-self-test" {
		return errRoundTrip
	}
	return nil
}
