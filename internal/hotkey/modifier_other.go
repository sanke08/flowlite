//go:build !darwin

package hotkey

// ModifierHeld always reports false outside macOS: there is no Right-Shift
// chord support here yet, so the history-panel gesture never fires.
func ModifierHeld() bool { return false }
