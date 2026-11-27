package helper

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

// coreCommand prepares the core: as the system for the tunnel, which needs
// the rights to make the device, and as the user for a proxy. Caller holds mu.
func (s *Supervisor) coreCommand(tunnel bool) (*exec.Cmd, error) {
	if tunnel || s.UserSID == "" {
		cmd := exec.Command(s.CorePath, "-home", s.HomeDir)
		cmd.SysProcAttr = hidden()
		return cmd, nil
	}
	// The files of a core that runs as the user are the user's.
	home := filepath.Join(s.HomeDir, "user")
	if err := secure(home, s.UserSID, "full"); err != nil {
		return nil, err
	}
	cmd := exec.Command(s.CorePath, "-home", home)
	token, err := asUser(cmd, s.UserSID)
	if err != nil {
		return nil, err
	}
	// The process holds its own copy once it starts.
	s.closeAfterStart = func() { token.Close() }
	return cmd, nil
}

// passVerdicts hands the core the pipe it answers on: a child on Windows
// inherits handles, not descriptors, and is told which one to use.
func passVerdicts(cmd *exec.Cmd, verdicts *os.File) error {
	handle := windows.Handle(verdicts.Fd())
	if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
		return err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.AdditionalInheritedHandles = append(cmd.SysProcAttr.AdditionalInheritedHandles, syscall.Handle(handle))
	cmd.Args = append(cmd.Args, "-verdicts", strconv.FormatUint(uint64(handle), 10))
	return nil
}
