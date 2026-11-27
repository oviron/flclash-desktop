package app

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// CoreName is the file name of the core binary.
const CoreName = "flclash-core.exe"

// hide keeps the core from flashing a console window over the app.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
