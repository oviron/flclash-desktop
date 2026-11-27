package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/bridge"
	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/model"
)

// noHelper points the helper's address where nothing listens: as on a
// computer it was never installed on.
func noHelper(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("FLCLASH_HELPER_SOCK", fmt.Sprintf(`\\.\pipe\flclash-test-none-%d`, time.Now().UnixNano()))
		return
	}
	// A socket path is short: t.TempDir with a long test name is too long.
	dir, err := os.MkdirTemp("", "fl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("FLCLASH_HELPER_SOCK", filepath.Join(dir, "none.sock"))
}

// modeAgent records the modes the engine was asked for.
func modeAgent(install func() error) (*Agent, func() []string) {
	var mu sync.Mutex
	var modes []string
	m := model.New("en")
	m.Call = func(r bridge.Request) bridge.Reply {
		if r.Op == "mode" {
			mu.Lock()
			modes = append(modes, r.Text)
			mu.Unlock()
		}
		return bridge.Reply{}
	}
	a := &Agent{Model: m, InstallHelper: install}
	return a, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), modes...)
	}
}

func setMode(t *testing.T, a *Agent, mode string) bridge.Reply {
	t.Helper()
	got := make(chan bridge.Reply, 1)
	a.SetMode(mode, func(r bridge.Reply) { got <- r })
	select {
	case r := <-got:
		return r
	case <-time.After(5 * time.Second):
		t.Fatalf("no reply to mode %s", mode)
	}
	return bridge.Reply{}
}

// The proxy modes need no helper: nothing is installed for them.
func TestSetModeProxyNeedsNoHelper(t *testing.T) {
	noHelper(t)
	a, modes := modeAgent(func() error { t.Error("the helper was installed for the port"); return nil })
	setMode(t, a, helper.ModeProxy)
	setMode(t, a, helper.ModeSystem)
	if got := modes(); len(got) != 2 || got[0] != helper.ModeProxy || got[1] != helper.ModeSystem {
		t.Fatalf("modes %v", got)
	}
}

// The tunnel installs the helper first, and the mode changes once it is there.
func TestSetModeTunnelInstallsTheHelper(t *testing.T) {
	noHelper(t)
	installs := 0
	a, modes := modeAgent(func() error { installs++; return nil })
	setMode(t, a, helper.ModeTUN)
	if installs != 1 {
		t.Fatalf("installed %d times", installs)
	}
	if got := modes(); len(got) != 1 || got[0] != helper.ModeTUN {
		t.Fatalf("modes %v", got)
	}
}

// Declined at the system's prompt: the mode stays, and nothing is reported.
func TestSetModeTunnelCancelled(t *testing.T) {
	noHelper(t)
	a, modes := modeAgent(func() error { return ErrCancelled })
	if r := setMode(t, a, helper.ModeTUN); r.Error != "" {
		t.Fatalf("a cancel answered %q", r.Error)
	}
	if got := modes(); len(got) != 0 {
		t.Fatalf("the mode changed after a cancel: %v", got)
	}
	if p := a.Model.Snapshot().Problem; p != "" {
		t.Fatalf("a cancel reported %q", p)
	}
}

// A failed install is shown, and the mode stays.
func TestSetModeTunnelInstallFails(t *testing.T) {
	noHelper(t)
	a, modes := modeAgent(func() error { return errors.New("no room on the disk") })
	setMode(t, a, helper.ModeTUN)
	if got := modes(); len(got) != 0 {
		t.Fatalf("the mode changed after a failed install: %v", got)
	}
	if p := a.Model.Snapshot().Problem; p != "no room on the disk" {
		t.Fatalf("problem %q", p)
	}
}

// While the system's prompt is up, another choice waits for its answer and
// changes nothing.
func TestSetModeOneInstallAtATime(t *testing.T) {
	noHelper(t)
	asked, answer := make(chan struct{}), make(chan error)
	a, modes := modeAgent(func() error { close(asked); return <-answer })
	first := make(chan bridge.Reply, 1)
	a.SetMode(helper.ModeTUN, func(r bridge.Reply) { first <- r })
	select {
	case <-asked:
	case r := <-first:
		t.Fatalf("answered before the prompt: %+v", r)
	case <-time.After(5 * time.Second):
		t.Fatal("the helper was not asked for")
	}
	setMode(t, a, helper.ModeProxy)
	if got := modes(); len(got) != 0 {
		t.Fatalf("a mode was taken during the prompt: %v", got)
	}
	answer <- nil
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("no reply after the install")
	}
	if got := modes(); len(got) != 1 || got[0] != helper.ModeTUN {
		t.Fatalf("modes %v", got)
	}
}

// The helper is removed only when nothing runs: removing it would stop what
// does without saying so.
func TestRemoveHelper(t *testing.T) {
	var running atomic.Bool
	removed := 0
	m := model.New("en")
	m.Call = func(r bridge.Request) bridge.Reply {
		switch r.Op {
		case "snapshot":
			return bridge.Reply{View: &app.View{}}
		case "status":
			return bridge.Reply{Status: &bridge.Status{Installed: true, Running: running.Load()}}
		}
		return bridge.Reply{}
	}
	a := &Agent{Model: m, RemoveHelper: func() error { removed++; return nil }}
	load := func() {
		t.Helper()
		m.Refresh()
		deadline := time.Now().Add(5 * time.Second)
		for !m.Snapshot().Loaded || m.Snapshot().Status.Running != running.Load() {
			if time.Now().After(deadline) {
				t.Fatal("the model did not load")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	running.Store(true)
	load()
	if err := a.removeHelper(); err == nil || removed != 0 {
		t.Fatalf("removed while running: %v, %d", err, removed)
	}
	running.Store(false)
	load()
	if err := a.removeHelper(); err != nil || removed != 1 {
		t.Fatalf("not removed when off: %v, %d", err, removed)
	}
	a.RemoveHelper = func() error { return ErrCancelled }
	if err := a.removeHelper(); err != nil {
		t.Fatalf("a cancel answered %v", err)
	}
}
