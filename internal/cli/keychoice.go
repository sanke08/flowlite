package cli

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"golang.org/x/term"

	"github.com/sanke08/flowlite/internal/hotkey"
)

const keyGestures = "Hold to talk · double-tap for hands-free, press again to stop · triple-tap pastes your last transcript · Esc cancels."

// customKey is the list row that switches to pressing the key instead.
const customKey = "custom"

// chooseKey asks for the dictation key: pick one of the usual keys from a
// list, or choose "Press a custom key…" and press any key, or a few held
// together, shown live and saved with Enter. Esc in the capture goes back to
// the list; Esc in the list returns huh.ErrUserAborted.
func chooseKey(current string) (string, error) {
	for {
		key, err := pickKeyFromList(current)
		if err != nil || key != customKey {
			return key, err
		}
		key, err = captureKey(current)
		if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, hotkey.ErrNotTrusted) {
			fmt.Println()
			continue // back to the list
		}
		return key, err
	}
}

// captureKey listens for the key or keys the user presses. It returns
// hotkey.ErrNotTrusted, after saying why, when the keyboard cannot be
// listened to (no Accessibility yet).
func captureKey(current string) (string, error) {
	events := make(chan hotkey.RawKey, 64)
	capture, err := hotkey.StartCapture(events)
	if err != nil {
		if runtime.GOOS == "darwin" {
			fmt.Println(warn("  macOS isn't letting " + hostApp() + " see the keyboard, so a pressed key can't be detected."))
			fmt.Println(dim("  Allow it under System Settings → Privacy & Security → Accessibility, then try again. For now, pick from the list."))
		} else {
			fmt.Println(warn("  Could not listen to the keyboard (" + err.Error() + "). Pick from the list instead."))
		}
		return "", hotkey.ErrNotTrusted
	}

	fmt.Println(bold("Custom dictation key") + dim("  now: "+hotkey.Label(current)))
	fmt.Println(dim(fmt.Sprintf("  Press the key you want — or up to %d held together, like Fn + Control. Enter saves · Esc goes back.", hotkey.MaxChord)))

	// Raw mode so the keys pressed here are not echoed over the prompt. The
	// tap only listens, so they still reach the terminal; drainInput throws
	// them away afterwards, or the confirming Enter would select the next
	// menu row.
	fd := int(os.Stdin.Fd())
	if st, err := term.MakeRaw(fd); err == nil {
		defer term.Restore(fd, st)
	}
	defer func() {
		capture.Stop()
		time.Sleep(80 * time.Millisecond) // let the terminal deliver the last keys
		drainInput()
	}()

	var rec hotkey.Recorder
	line := func(s string) { fmt.Print("\r\033[K  " + s) }
	show := func() {
		if held := rec.Held(); held != "" {
			line(bold(hotkey.Label(held)))
			return
		}
		combo := rec.Combo()
		if combo == "" {
			line(dim("waiting for a key… (Enter keeps " + hotkey.Label(current) + ")"))
			return
		}
		if err := checkChord(combo); err != nil {
			line(bold(hotkey.Label(combo)) + "  " + warn(err.Error()))
			return
		}
		line(bold(hotkey.Label(combo)) + dim("  Enter to save · press again to change"))
	}
	show()

	cancel := func() (string, error) {
		line(dim("back to the list"))
		fmt.Print("\r\n")
		return "", huh.ErrUserAborted
	}
	confirm := func() (string, bool) {
		combo := rec.Combo()
		if combo == "" {
			combo = current // nothing pressed: keep what is set
		}
		if checkChord(combo) != nil {
			show()
			return "", false
		}
		line(bold(hotkey.Label(combo)))
		fmt.Print("\r\n")
		return combo, true
	}

	// The terminal is polled as well as the tap. A tap can be created and
	// still hear nothing (Secure Keyboard Entry is on, or Accessibility was
	// granted without relaunching the terminal); raw mode has also turned
	// Ctrl+C off, so without this there would be no way out.
	started := time.Now()
	pressed := false
	tick := time.NewTicker(30 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case ev := <-events:
			switch {
			case ev.Name == "esc":
				if ev.Down {
					return cancel()
				}
			case ev.Name == "enter" || ev.Name == "keypad_enter":
				// Only a press made here counts: the Enter that opened this
				// prompt may still be coming up, or auto-repeating, when
				// listening starts.
				if !ev.Down || !pressed && time.Since(started) < 300*time.Millisecond {
					continue
				}
				if combo, ok := confirm(); ok {
					return combo, nil
				}
			default:
				pressed = true
				if rec.Feed(ev.Name, ev.Down) {
					show()
				}
			}
		case <-tick.C:
			in := pollInput()
			switch {
			case len(in) == 1 && in[0] == 0x1b, // a bare Esc; arrows arrive as ESC [ …
				len(in) > 0 && in[0] == 0x03: // Ctrl+C
				return cancel()
			case len(in) > 0 && (in[len(in)-1] == '\r' || in[len(in)-1] == '\n'):
				if combo, ok := confirm(); ok {
					return combo, nil
				}
			}
		}
	}
}

// checkChord is hotkey.Check on a chord's spelling.
func checkChord(chord string) error {
	_, err := hotkey.Parse(chord)
	return err
}

// pickKeyFromList offers the usual keys, the current one if it is a custom
// chord, and the row that switches to pressing a custom key.
func pickKeyFromList(current string) (string, error) {
	opts := make([]huh.Option[string], 0)
	names := hotkey.Names()
	if !contains(names, current) {
		names = append([]string{current}, names...) // keep a custom chord selectable
	}
	for _, n := range names {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%-22s %s", hotkey.Label(n), dim(n)), n))
	}
	opts = append(opts, huh.NewOption(fmt.Sprintf("%-22s %s", "Press a custom key…",
		dim(fmt.Sprintf("any key, or up to %d together like Fn + Control", hotkey.MaxChord))), customKey))
	key := current
	if err := runPrompt(huh.NewSelect[string]().
		Title("Dictation key").
		Description(keyGestures).
		Options(opts...).Value(&key)); err != nil {
		return "", err
	}
	return key, nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// printKeyAdvice prints the caution for a chord that works but has a catch.
func printKeyAdvice(chord string) {
	if a := hotkey.Advice(chord); a != "" {
		fmt.Println(dim("  tip: " + a))
	}
}

// runPrompt runs one prompt. Esc backs out of it, the same as Ctrl+C: both return
// huh.ErrUserAborted, which a settings row treats as "back to the menu" and
// the menu itself as "leave".
func runPrompt(f huh.Field) error {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"))
	return huh.NewForm(huh.NewGroup(f)).WithShowHelp(false).WithKeyMap(km).Run()
}
