package hotkey

// KeyEvent is a raw press or release, already classified for the machine.
type KeyEvent struct {
	Kind KeyKind
	Down bool
}

// RawKey is one key going down or up, by name, as seen while capturing.
type RawKey struct {
	Name string
	Down bool
}
