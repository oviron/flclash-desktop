package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/oviron/flclash-desktop/internal/build"
	"github.com/oviron/flclash-desktop/internal/store"
	"github.com/oviron/flclash-desktop/internal/update"
)

// uninstallKey is where the installer records this app; the GUID is the
// AppId of windows\installer.iss.
const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\{6F1E3C2A-6B8D-4C55-9C1F-3A7E2D9B5F41}_is1`

// startUpdater looks for new releases. The installed copy installs them by
// running the new installer; any other copy, a portable one, offers the
// release page.
func (h *host) startUpdater() {
	dir, err := store.Dir()
	if err != nil {
		return
	}
	u := &update.Updater{
		Current: build.Version,
		Release: build.Released(),
		Dir:     dir,
		Lang:    h.lang,
		Asset:   h.updateAsset,
		Apply:   h.applyUpdate,
		Proxy:   h.agent.LocalProxy,
		Changed: h.agent.Changed,
	}
	h.agent.Updater = u
	u.Start()
}

// installed reports whether this copy is the one the installer put in place.
func (h *host) installed() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	place, _, err := key.GetStringValue("InstallLocation")
	return err == nil && strings.EqualFold(filepath.Clean(place), filepath.Clean(h.dir))
}

func (h *host) updateAsset(version string) string {
	if !h.installed() {
		return ""
	}
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	return "FlClash-" + version + "-windows-" + arch + "-setup.exe"
}

// applyUpdate runs the new installer without its pages and quits: the
// installer replaces the files and, asked with /update=1, starts the app
// again.
func (h *host) applyUpdate(path, _ string) error {
	cmd := exec.Command(path, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CLOSEAPPLICATIONS", "/update=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return err
	}
	go h.quit()
	return nil
}

// updateNow installs the new release, or opens its page for a copy that
// cannot install it.
func (h *host) updateNow() {
	u := h.agent.Updater
	if u == nil {
		return
	}
	s := u.State()
	if !s.CanInstall {
		if s.Page != "" {
			openURL(s.Page)
		}
		return
	}
	go u.Install(context.Background())
}

// openURL opens an address in the person's browser.
func openURL(address string) {
	verb, _ := windows.UTF16PtrFromString("open")
	target, err := windows.UTF16PtrFromString(address)
	if err != nil {
		return
	}
	windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}
