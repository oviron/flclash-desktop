//go:build !windows

package app

import "os/exec"

// CoreName is the file name of the core binary.
const CoreName = "flclash-core"

func hide(*exec.Cmd) {}
