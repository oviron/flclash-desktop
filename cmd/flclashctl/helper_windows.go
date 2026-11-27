package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/oviron/flclash-desktop/internal/helper"
)

const serviceName = "FlClashHelper"

// helperDir is where the service runs from: only administrators may change
// what is there, since the system runs it.
func helperDir() string {
	base := os.Getenv("ProgramFiles")
	if base == "" {
		base = `C:\Program Files`
	}
	return filepath.Join(base, "FlClash", "Helper")
}

func cmdHelper(args []string) error {
	if len(args) == 0 || (args[0] != "install" && args[0] != "uninstall") {
		return errors.New("usage (as administrator): flclashctl helper install [-sid SID] | uninstall")
	}
	flags := flag.NewFlagSet("helper", flag.ContinueOnError)
	// The app installs through the administrator prompt; an administrator
	// other than the user may answer it, so the app says whom to serve.
	sidFlag := flags.String("sid", "", "the user the helper serves; default: the one running this")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		return fmt.Errorf("run it as administrator: flclashctl helper %s", args[0])
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	if s, err := m.OpenService(serviceName); err == nil {
		stopService(s)
		if args[0] == "uninstall" {
			err := s.Delete()
			s.Close()
			if err != nil {
				return err
			}
			for _, path := range []string{helperDir(), helper.BaseDir()} {
				if err := os.RemoveAll(path); err != nil {
					return err
				}
			}
			fmt.Println("the helper is removed")
			return nil
		}
		s.Close()
	} else if args[0] == "uninstall" {
		return nil
	}

	sid := *sidFlag
	if sid == "" {
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			return err
		}
		sid = user.User.Sid.String()
	}
	if _, err := windows.StringToSid(sid); err != nil {
		return fmt.Errorf("%q is not a SID: %w", sid, err)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(helperDir(), 0o755); err != nil {
		return err
	}
	for _, name := range []string{"flclash-helper.exe", "flclash-core.exe"} {
		if err := copyFile(filepath.Join(filepath.Dir(self), name), filepath.Join(helperDir(), name)); err != nil {
			return fmt.Errorf("%s must be next to flclashctl: %w", name, err)
		}
	}
	exe := filepath.Join(helperDir(), "flclash-helper.exe")
	core := filepath.Join(helperDir(), "flclash-core.exe")
	config := mgr.Config{
		DisplayName:  "FlClash Helper",
		Description:  "Runs the FlClash core with the rights to capture traffic, and puts the network back when it stops.",
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}
	s, err := m.OpenService(serviceName)
	if err == nil {
		current, err := s.Config()
		if err != nil {
			s.Close()
			return err
		}
		current.DisplayName, current.Description, current.StartType = config.DisplayName, config.Description, config.StartType
		current.BinaryPathName = syscall.EscapeArg(exe) + " -sid " + sid + " -core " + syscall.EscapeArg(core)
		if err := s.UpdateConfig(current); err != nil {
			s.Close()
			return err
		}
	} else {
		s, err = m.CreateService(serviceName, exe, config, "-sid", sid, "-core", core)
		if err != nil {
			return err
		}
	}
	defer s.Close()
	// A helper that dies comes back, as launchd brings it back on macOS.
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: time.Second},
		{Type: mgr.ServiceRestart, Delay: 3 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
	}, 24*60*60)
	if err := s.Start(); err != nil {
		return err
	}
	// Installed means it answers, not that the files are in place.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if status, err := helper.Call(helper.Request{Op: helper.OpStatus}, 3*time.Second); err == nil && status.Version == helper.Version {
			fmt.Println("the helper is installed and running")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the helper was installed but does not answer; see %s", helper.LogPath())
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// stopService stops the service and waits for it, so that its files can be replaced.
func stopService(s *mgr.Service) {
	status, err := s.Control(svc.Stop)
	if err != nil {
		return
	}
	deadline := time.Now().Add(20 * time.Second)
	for status.State != svc.Stopped && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if status, err = s.Query(); err != nil {
			return
		}
	}
}

func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	// A new file under a new name, then a rename: the program is never half-written.
	tmp := to + ".new"
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, to)
}
