//go:build darwin

package overlay

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore
#include <stdlib.h>
#include <stdbool.h>
void flowlite_overlay_set_position(int pos);
void flowlite_overlay_show(int state, const char *text);
void flowlite_overlay_set_state(int state, const char *text);
void flowlite_overlay_set_level(float level);
void flowlite_overlay_hide(void);
bool flowlite_overlay_snapshot(const char *path);
bool flowlite_overlay_snapshot_window(const char *path);
void flowlite_overlay_show_history(const char *entriesJSON);
void flowlite_overlay_hide_history(void);
bool flowlite_overlay_history_open(void);
void flowlite_overlay_history_set_query(const char *query);
bool flowlite_overlay_history_has_key(void);

// Called back from overlay_darwin.m when the user acts on the history panel.
extern void flowliteHistoryPick(int index);
extern void flowliteHistoryClosed(void);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"sync"
	"unsafe"

	"github.com/sanke08/flowlite/internal/mainloop"
)

func applyPosition(code int) {
	mainloop.Dispatch(func() { C.flowlite_overlay_set_position(C.int(code)) })
}

// Show makes the pill visible in the given state. Main-thread work is
// dispatched; callers may be on any goroutine. text is only ever drawn for
// the terminal Error state — every other state shows no words.
func Show(s State, text string) {
	mainloop.Dispatch(func() {
		c := C.CString(text)
		defer C.free(unsafe.Pointer(c))
		C.flowlite_overlay_show(C.int(s), c)
	})
}

// SetState changes appearance without repositioning. Pasted and Cancelled
// start the fade-out immediately; Error pulses red twice and then fades.
func SetState(s State, text string) {
	mainloop.Dispatch(func() {
		c := C.CString(text)
		defer C.free(unsafe.Pointer(c))
		C.flowlite_overlay_set_state(C.int(s), c)
	})
}

// SetLevel pushes a 0–1 microphone level into the waveform.
func SetLevel(level float64) {
	mainloop.Dispatch(func() { C.flowlite_overlay_set_level(C.float(level)) })
}

// Hide fades the pill out.
func Hide() {
	mainloop.Dispatch(func() { C.flowlite_overlay_hide() })
}

// Snapshot renders the pill's current appearance directly to a PNG file at
// path, without capturing the screen. This is useful for visually inspecting
// or testing the pill's rendering (e.g. from a test or a manual invocation),
// since it does not require the pill to actually be shown on screen.
func Snapshot(path string) error {
	var ok bool
	mainloop.DispatchSync(func() {
		c := C.CString(path)
		defer C.free(unsafe.Pointer(c))
		ok = bool(C.flowlite_overlay_snapshot(c))
	})
	if !ok {
		return errors.New("could not render the pill")
	}
	return nil
}

// SnapshotWindow is Snapshot for the whole panel: it renders the window's
// content view at its CURRENT size (the pill, or the grown history panel)
// including every AppKit subview, so the history panel's real layout can be
// inspected offline the same way Snapshot inspects the pill's drawing.
func SnapshotWindow(path string) error {
	var ok bool
	mainloop.DispatchSync(func() {
		c := C.CString(path)
		defer C.free(unsafe.Pointer(c))
		ok = bool(C.flowlite_overlay_snapshot_window(c))
	})
	if !ok {
		return errors.New("could not render the panel")
	}
	return nil
}

// ---- history panel --------------------------------------------------------

var (
	histMu      sync.Mutex
	histOnPick  func(int)
	histOnClose func()
)

// historyRow is the wire shape sent to Cocoa: a dumb renderer, not a
// formatter. time and preview are already display-ready strings so the
// Objective-C side never has to know FlowLite's formatting conventions.
type historyRow struct {
	Index   int    `json:"index"`
	Time    string `json:"time"`
	Preview string `json:"preview"`
}

// showHistory marshals entries to JSON (time and the transcript text, both
// pre-formatted here so Cocoa only has to draw them) and asks the Cocoa side
// to morph the pill into the scrollable list. onPick/onClose are stored under
// histMu and invoked from flowliteHistoryPick/flowliteHistoryClosed, which
// Cocoa calls back on the main thread.
func showHistory(entries []HistoryEntry, onPick func(int), onClose func()) {
	rows := make([]historyRow, len(entries))
	for i, e := range entries {
		rows[i] = historyRow{
			Index:   i,
			Time:    e.Time.Local().Format("15:04"),
			Preview: e.Text,
		}
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return
	}

	histMu.Lock()
	histOnPick = onPick
	histOnClose = onClose
	histMu.Unlock()

	mainloop.Dispatch(func() {
		c := C.CString(string(data))
		defer C.free(unsafe.Pointer(c))
		C.flowlite_overlay_show_history(c)
	})
}

// hideHistory morphs the panel back down to the plain pill.
func hideHistory() {
	mainloop.Dispatch(func() { C.flowlite_overlay_hide_history() })
}

// isHistoryOpen reports whether the history panel is currently showing.
func isHistoryOpen() bool {
	var open bool
	mainloop.DispatchSync(func() { open = bool(C.flowlite_overlay_history_open()) })
	return open
}

// setHistoryQuery types query into the panel's search field (replacing what
// is there) and re-filters the rows.
func setHistoryQuery(query string) {
	mainloop.Dispatch(func() {
		c := C.CString(query)
		defer C.free(unsafe.Pointer(c))
		C.flowlite_overlay_history_set_query(c)
	})
}

// historyHasKey reports whether the panel currently holds keyboard focus.
func historyHasKey() bool {
	var key bool
	mainloop.DispatchSync(func() { key = bool(C.flowlite_overlay_history_has_key()) })
	return key
}

//export flowliteHistoryPick
func flowliteHistoryPick(index C.int) {
	histMu.Lock()
	cb := histOnPick
	histMu.Unlock()
	if cb != nil {
		cb(int(index))
	}
}

//export flowliteHistoryClosed
func flowliteHistoryClosed() {
	histMu.Lock()
	cb := histOnClose
	histOnPick = nil
	histOnClose = nil
	histMu.Unlock()
	if cb != nil {
		cb()
	}
}
