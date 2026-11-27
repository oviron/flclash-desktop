//go:build !darwin

package helper

// NewSystemProxy returns nil where setting the system proxy is not written yet.
func NewSystemProxy() SystemProxy { return nil }
