//go:build !darwin && !windows

package main

import "errors"

func cmdHelper(_ []string) error {
	return errors.New("the helper is implemented for macOS only so far")
}
