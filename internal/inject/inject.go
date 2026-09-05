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
	// The target app reads the clipboard when *it* handles the paste, which is
	// asynchronous from our point of view: we post Cmd+V and have no way to
	// learn when the read happened. Restore too early and a slow app — Electron
	// under load, a remote desktop, an editor mid-index — reads the restored
	// clipboard and inserts the user's old text into their document instead of
	// the transcript. That failure is silent and lands in real work, so this
	// waits long enough to be dull rather than tight enough to be clever.
	restoreDelay = 600 * time.Millisecond
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
	ours := clipboardSerial()

	// Put the old clipboard back only if ours is still the one there. If the
	// user pressed Cmd+C while we were waiting, that copy is newer than
	// anything we know about and overwriting it would throw away something
	// they just did on purpose.
	//
	// This has to run on the failure path too. When the paste keystroke is
	// refused — Accessibility switched off mid-session — nothing was pasted,
	// so the wait is pointless, but leaving the transcript sitting on the
	// clipboard would quietly destroy whatever the user had copied, on every
	// dictation, for as long as the permission stays off.
	putBack := func(wait time.Duration) {
		if !restore || !hadPrevious {
			return
		}
		time.Sleep(wait)
		if clipboardSerial() == ours {
			_ = clipboardSet(previous)
		}
	}

	time.Sleep(preDelay)
	if err := pasteKeystroke(); err != nil {
		putBack(0)
		return err
	}
	putBack(restoreDelay)
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

// SetClipboard puts text on the clipboard without pasting. `flowlite settings
// → Recent transcripts` uses it: the terminal has focus there, so a paste
// would land in the wrong place.
func SetClipboard(text string) error {
	return clipboardSet(text)
}
