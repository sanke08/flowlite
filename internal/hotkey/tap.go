package hotkey

// KeyEvent is a raw press or release, already classified for the machine.
type KeyEvent struct {
	Kind KeyKind
	Down bool
}
