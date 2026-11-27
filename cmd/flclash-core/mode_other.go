//go:build !windows

package main

// otherMode runs what the core does besides being the core; nothing here.
func otherMode() bool { return false }
