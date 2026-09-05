//go:build !darwin && !windows

package overlay

// Linux pill is not implemented yet; these keep the daemon portable.
func Show(s State, text string)        {}
func SetState(s State, text string)    {}
func SetLevel(level float64)           {}
func Hide()                            {}
func Snapshot(path string) error       { return nil }
func SnapshotWindow(path string) error { return nil }
func applyPosition(code int)           {}

func showHistory(entries []HistoryEntry, onPick func(int), onClose func()) {}
func hideHistory()                                                         {}
func isHistoryOpen() bool                                                  { return false }
func setHistoryQuery(query string)                                         {}
func historyHasKey() bool                                                  { return false }
