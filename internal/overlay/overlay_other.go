//go:build !darwin && !windows

package overlay

// Windows/Linux pill is not implemented yet; these keep the daemon portable.
func Show(s State, text string)     {}
func SetState(s State, text string) {}
func SetLevel(level float64)        {}
func Hide()                         {}
func Snapshot(path string) error { return nil }
