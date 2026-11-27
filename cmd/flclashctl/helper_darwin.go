package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/oviron/flclash-desktop/internal/helper"
)

const (
	helperLabel = "io.github.oviron.flclash.helper"
	helperDir   = "/Library/PrivilegedHelperTools"
	helperPlist = "/Library/LaunchDaemons/" + helperLabel + ".plist"
)

func cmdHelper(args []string) error {
	if len(args) == 0 || (args[0] != "install" && args[0] != "uninstall") {
		return errors.New("usage: sudo flclashctl helper install [-uid UID] | uninstall")
	}
	flags := flag.NewFlagSet("helper", flag.ContinueOnError)
	// The app installs through the system's administrator prompt, which sets
	// no SUDO_UID, so it says whom to serve.
	uidFlag := flags.Int("uid", 0, "the user the helper serves; default: the one who ran sudo")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("run it with sudo: sudo flclashctl helper %s", args[0])
	}
	// Stop what runs now; an error only means there was nothing to stop.
	_ = exec.Command("launchctl", "bootout", "system/"+helperLabel).Run()
	if args[0] == "uninstall" {
		// A helper that was killed may have left its firewall rules and the
		// system proxy behind. The files go either way; what could not be
		// put back is the answer, so the app says it.
		guardErr := helper.NewGuard().Release()
		proxyErr := helper.NewSystemProxy().Clear()
		for _, path := range []string{helperPlist, filepath.Join(helperDir, "flclash-helper"), filepath.Join(helperDir, "flclash-core"), helper.SocketPath(), helper.LogPath()} {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := os.RemoveAll(helper.StateDir()); err != nil {
			return err
		}
		return errors.Join(guardErr, proxyErr)
	}

	// The helper serves the user who ran sudo, not root.
	uid := *uidFlag
	if uid == 0 {
		uid, _ = strconv.Atoi(os.Getenv("SUDO_UID"))
	}
	if uid <= 0 {
		return errors.New("cannot tell which user to serve: run through sudo from your own account, or pass -uid")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(helperDir, 0o755); err != nil {
		return err
	}
	// Root runs these copies; they sit where only root can replace them.
	for _, name := range []string{"flclash-helper", "flclash-core"} {
		if err := copyFile(filepath.Join(filepath.Dir(self), name), filepath.Join(helperDir, name)); err != nil {
			return fmt.Errorf("%s must be next to flclashctl: %w", name, err)
		}
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s/flclash-helper</string>
		<string>-uid</string><string>%d</string>
		<string>-core</string><string>%s/flclash-core</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, helperLabel, helperDir, uid, helperDir, helper.LogPath(), helper.LogPath())
	if err := os.WriteFile(helperPlist, []byte(plist), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("launchctl", "bootstrap", "system", helperPlist).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	// Installed means it answers, not that the files are in place.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(helper.SocketPath()); err == nil {
			fmt.Println("the helper is installed and running")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the helper was installed but did not start; see %s", helper.LogPath())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	// A new file under a new name, then a rename: the running binary is never half-written.
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
	if err := os.Chown(tmp, 0, 0); err != nil {
		return err
	}
	return os.Rename(tmp, to)
}
