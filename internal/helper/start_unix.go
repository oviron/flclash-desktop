//go:build !windows

package helper

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// coreCommand prepares the core: as root with the core's own group for the
// tunnel, as the user for a proxy. Caller holds mu.
func (s *Supervisor) coreCommand(tunnel bool) (*exec.Cmd, error) {
	home := s.HomeDir
	var credential *syscall.Credential
	if tunnel {
		if s.CoreGID != 0 {
			credential = &syscall.Credential{Uid: 0, Gid: uint32(s.CoreGID), NoSetGroups: true}
		}
	} else {
		// The files of a core that runs as the user are the user's.
		home = filepath.Join(home, "user")
		if err := os.MkdirAll(home, 0o700); err != nil {
			return nil, err
		}
		if s.UserUID > 0 {
			gid := s.UserUID
			if u, err := user.LookupId(strconv.Itoa(s.UserUID)); err == nil {
				if n, err := strconv.Atoi(u.Gid); err == nil {
					gid = n
				}
			}
			if err := os.Chown(home, s.UserUID, gid); err != nil {
				return nil, err
			}
			credential = &syscall.Credential{Uid: uint32(s.UserUID), Gid: uint32(gid), NoSetGroups: true}
		}
	}
	cmd := exec.Command(s.CorePath, "-home", home)
	if credential != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	}
	return cmd, nil
}

// passVerdicts hands the core the pipe it answers on: the child sees it as
// descriptor 3.
func passVerdicts(cmd *exec.Cmd, verdicts *os.File) error {
	cmd.ExtraFiles = []*os.File{verdicts}
	cmd.Args = append(cmd.Args, "-verdicts", "3")
	return nil
}
