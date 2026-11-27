//go:build !darwin && !windows

package helper

// NewGuard returns nil where the guard is not written yet.
func NewGuard() Guard { return nil }
