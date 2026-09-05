package inject

import "testing"

// Exercises the fixed paths against the live macOS pasteboard.
func TestClipboardSerialMovesOnWrite(t *testing.T) {
	prev, had := clipboardGet()

	a := clipboardSerial()
	if err := clipboardSet("flowlite-serial-test"); err != nil {
		t.Fatalf("clipboardSet now reports failures, and it failed: %v", err)
	}
	b := clipboardSerial()
	t.Logf("clipboardSerial before=%d after=%d (moved=%v)", a, b, b != a)
	if b == a {
		t.Errorf("serial did not move after a write — the restore guard would be useless")
	}

	got, _ := clipboardGet()
	if got != "flowlite-serial-test" {
		t.Errorf("read back %q", got)
	}

	// Simulate the guard: someone else writes after us, so we must NOT restore.
	ours := clipboardSerial()
	_ = clipboardSet("someone-else-copied-this")
	if clipboardSerial() == ours {
		t.Errorf("guard broken: a third-party write did not move the serial")
	} else {
		t.Logf("guard works: serial moved, so the old clipboard would be left alone")
	}

	// Always put the machine's clipboard back. `had` is false when it held an
	// image or a file rather than text, and leaving our probe string there
	// would be a real, if small, act of vandalism on every `go test ./...`.
	if had {
		_ = clipboardSet(prev)
	} else {
		_ = clipboardSet("")
	}
}
