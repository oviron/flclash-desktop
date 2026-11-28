package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/oviron/flclash-desktop/internal/agent"
	"github.com/oviron/flclash-desktop/internal/build"
	"github.com/oviron/flclash-desktop/internal/say"
	"github.com/oviron/flclash-desktop/internal/store"
	"github.com/oviron/flclash-desktop/internal/update"
)

// startUpdater looks for new releases. The app replaces its own bundle
// where it can write beside it; elsewhere, run from a disk image or from a
// folder it may not write to, it offers the release page.
func startUpdater(a *agent.Agent, lang string) {
	dir, err := store.Dir()
	if err != nil {
		return
	}
	u := &update.Updater{
		Current: build.Version,
		Release: build.Released(),
		Dir:     dir,
		Lang:    lang,
		Asset:   updateAsset,
		Proxy:   a.LocalProxy,
		Changed: a.Changed,
	}
	u.Apply = func(path, version string) error { return applyUpdate(a, path, version) }
	a.Updater = u
	u.Start()
}

// bundle is the path of the running app, or "" when it is not run from one.
func bundle() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	i := strings.Index(exe, ".app/Contents/MacOS/")
	if i < 0 {
		return ""
	}
	return exe[:i+len(".app")]
}

func updateAsset(version string) string {
	app := bundle()
	// A disk image, or a copy macOS runs from a hidden place until it is
	// moved: not where the app stays.
	if app == "" || strings.HasPrefix(app, "/Volumes/") || strings.Contains(app, "/AppTranslocation/") {
		return ""
	}
	if syscall.Access(filepath.Dir(app), 2 /* W_OK */) != nil {
		return ""
	}
	return "FlClash-" + version + "-macos-arm64.zip"
}

// swap waits for the app to quit, puts the new bundle in its place, opens
// it, and throws the old one away; should the move fail, the old one comes
// back. While no app stands in the bundle's place, the folder with both is
// kept, so a copy that works is never thrown away. $1 is the app's process,
// $2 the bundle, $3 the folder the new one was unpacked in.
const swap = `while kill -0 "$1" 2>/dev/null; do sleep 0.2; done
if mv "$2" "$3/old.app"; then
  mv "$3/FlClash.app" "$2" || mv "$3/old.app" "$2"
fi
[ -d "$2" ] || exit 1
/usr/bin/open "$2"
case "$3" in */.FlClash-update-*) /bin/rm -rf "$3" ;; esac
`

// applyUpdate unpacks the verified archive next to the app, checks the new
// bundle, and leaves the swap to a process that outlives the app, which is
// told to quit.
func applyUpdate(a *agent.Agent, path, version string) error {
	app := bundle()
	if app == "" {
		return errors.New("the app does not run from a bundle")
	}
	stage, err := os.MkdirTemp(filepath.Dir(app), ".FlClash-update-")
	if err != nil {
		return err
	}
	fail := func(err error) error {
		os.RemoveAll(stage)
		return err
	}
	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", path, stage).CombinedOutput(); err != nil {
		return fail(say.Errorf("the archive did not unpack: %s", strings.TrimSpace(string(out))))
	}
	next := filepath.Join(stage, "FlClash.app")
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", next).CombinedOutput(); err != nil {
		return fail(say.Errorf("the new app's signature does not check: %s", strings.TrimSpace(string(out))))
	}
	short, _, _ := strings.Cut(version, "-")
	got, _ := exec.Command("/usr/bin/defaults", "read", filepath.Join(next, "Contents", "Info"), "CFBundleShortVersionString").Output()
	if strings.TrimSpace(string(got)) != short {
		return fail(say.Errorf("the archive holds version %s, not %s", strings.TrimSpace(string(got)), short))
	}
	os.Remove(path)
	cmd := exec.Command("/bin/sh", "-c", swap, "sh", strconv.Itoa(os.Getpid()), app, stage)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fail(err)
	}
	// The window quits the app the usual way, which turns off what runs.
	a.Tell("quit")
	return nil
}
