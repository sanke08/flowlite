//go:build !darwin

package permissions

// Needed: Windows and Linux do not gate global keyboard hooks.
func Needed() bool { return false }

// Trusted is always true where no gate exists.
func Trusted() bool { return true }

// Request is a no-op.
func Request() bool { return true }
