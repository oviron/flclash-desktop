//go:build !darwin

package main

import "github.com/oviron/flclash-desktop/internal/agent"

// startUpdater: the app updates itself only on macOS; the bridge is built
// for other systems only to be checked.
func startUpdater(*agent.Agent, string) {}
