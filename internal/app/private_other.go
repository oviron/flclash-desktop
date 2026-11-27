//go:build !windows

package app

import "os"

// createPrivate makes a new file in dir that only its owner may read.
func createPrivate(dir string) (*os.File, error) {
	// CreateTemp makes it 0600.
	return os.CreateTemp(dir, ".flclash-*")
}
