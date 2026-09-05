// Package inject puts text into whatever field currently has focus.
//
// Pasting is used rather than synthesising one key event per character: it is
// near-instant regardless of length, does not mangle non-ASCII text, and does
// not trigger autocomplete on every keystroke.
package inject

import (
	"sync"
	"time"
)

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
	//
	// The restore itself now runs in the background (see putBack/pending
	// below) rather than blocking Paste's return, so this delay no longer
	// costs the caller any wall-clock time — it only bounds how long the old
	// clipboard contents keep waiting to come back. Exported so a caller
	// like daemon.Close can size its WaitPending timeout relative to it
	// rather than picking an unrelated number.
	RestoreDelay = 600 * time.Millisecond
)

// pending tracks in-flight background clipboard restores (see putBack), so
// shutdown can wait for them instead of racing the process exit against a
// sleep that is no longer inline in Paste. See WaitPending.
var pending sync.WaitGroup

// Paste puts text on the clipboard, sends the paste shortcut, and (optionally)
// restores what was there before. It returns as soon as the keystroke itself
// has been sent — the clipboard restore, when there is one, continues in a
// background goroutine tracked by `pending` (see WaitPending) rather than
// blocking the caller for RestoreDelay.
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
	//
	// Runs in its own goroutine so Paste itself returns as soon as the
	// keystroke has been sent, rather than blocking the caller for the whole
	// wait. `pending` lets WaitPending (called from daemon.Close) give this
	// goroutine a bounded chance to finish before the process exits, since
	// nothing else waits for it now that Paste no longer blocks on it.
	putBack := func(wait time.Duration) {
		if !restore || !hadPrevious {
			return
		}
		pending.Add(1)
		go func() {
			defer pending.Done()
			time.Sleep(wait)
			if clipboardSerial() == ours {
				_ = clipboardSet(previous)
			}
		}()
	}

	time.Sleep(preDelay)
	if err := pasteKeystroke(); err != nil {
		putBack(0)
		return err
	}
	putBack(RestoreDelay)
	return nil
}

// WaitPending blocks until every in-flight background clipboard restore
// started by Paste (see putBack above) has finished, or until timeout
// elapses, whichever comes first — bounded so shutdown can never hang
// indefinitely on a stuck goroutine. daemon.Close calls this so the process
// never exits with a pending restore still waiting to run, which would
// silently leave the transcript on the clipboard instead of whatever the
// user had copied before dictating.
func WaitPending(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		pending.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
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
