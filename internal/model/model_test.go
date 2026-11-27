package model

import (
	"sync"
	"testing"
	"time"

	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/bridge"
	"github.com/oviron/flclash-desktop/internal/helper"
)

// engine is a fake engine: one server, and a tunnel that goes on and off.
type engine struct {
	mu      sync.Mutex
	running bool
	auto    bool
	ops     []string
}

func (e *engine) call(r bridge.Request) bridge.Reply {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ops = append(e.ops, r.Op)
	view := &app.View{Servers: []app.Server{{ID: "a", Name: "A"}}, Selected: "a", AutoConnect: e.auto}
	status := func() *bridge.Status {
		return &bridge.Status{Installed: true, Running: e.running, Captured: e.running, Server: "A", Mode: "proxy"}
	}
	switch r.Op {
	case "snapshot":
		return bridge.Reply{View: view}
	case "status":
		return bridge.Reply{Status: status()}
	case "on":
		e.running = true
		return bridge.Reply{Status: status()}
	case "off":
		e.running = false
		return bridge.Reply{Status: status()}
	case "delay":
		if r.Server == "a" {
			return bridge.Reply{Delay: 42}
		}
		return bridge.Reply{Error: "timeout"}
	case "stats":
		return bridge.Reply{Stats: &helper.Stats{Down: 10, Up: 1}}
	case "exit":
		return bridge.Reply{Exit: &helper.Exit{IP: "192.0.2.1", Country: "NL"}}
	}
	return bridge.Reply{}
}

func (e *engine) count(op string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, o := range e.ops {
		if o == op {
			n++
		}
	}
	return n
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("never: %s", what)
}

func TestAutoConnectOnce(t *testing.T) {
	e := &engine{auto: true}
	m := New("en")
	m.Call = e.call
	m.Refresh()
	eventually(t, "connected at start", func() bool { return m.Snapshot().Status.Running })
	m.Refresh()
	m.Refresh()
	time.Sleep(100 * time.Millisecond)
	if n := e.count("on"); n != 1 {
		t.Fatalf("turned on %d times", n)
	}
}

func TestToggleAndMeasure(t *testing.T) {
	e := &engine{}
	m := New("en")
	m.Call = e.call
	changes := 0
	var mu sync.Mutex
	cancel := m.OnChange(func() { mu.Lock(); changes++; mu.Unlock() })
	m.Refresh()
	eventually(t, "loaded", func() bool { return m.Snapshot().Loaded })
	if m.Snapshot().Status.Running {
		t.Fatal("connected without being asked")
	}
	m.Toggle()
	eventually(t, "on", func() bool { s := m.Snapshot(); return s.Status.Running && !s.Busy })
	m.Measure([]string{"a", "b"})
	eventually(t, "measured", func() bool {
		d := m.Snapshot().Delays
		return d["a"].State == "ms" && d["b"].State == "failed"
	})
	if got := m.Snapshot().Delays["a"].MS; got != 42 {
		t.Fatalf("delay %d", got)
	}
	m.WatchStats(true)
	eventually(t, "traffic and exit", func() bool {
		s := m.Snapshot()
		return s.Stats != nil && len(s.History) > 0 && s.Exit != nil && s.Exit.Country == "NL"
	})
	m.WatchStats(false)
	if s := m.Snapshot(); s.Stats != nil || len(s.History) != 0 {
		t.Fatal("traffic kept with no one watching")
	}
	m.Toggle()
	eventually(t, "off", func() bool { return !m.Snapshot().Status.Running })
	cancel()
	mu.Lock()
	defer mu.Unlock()
	if changes == 0 {
		t.Fatal("no change was told")
	}
}

// Quitting turns off after what was asked before it, and nothing asked after
// it turns the tunnel back on.
func TestQuitComesAfterQueuedChanges(t *testing.T) {
	e := &engine{}
	m := New("en")
	m.Call = e.call
	m.Change(bridge.Request{Op: "on"}, false, nil)
	m.Quit()
	m.Change(bridge.Request{Op: "on"}, false, nil)
	time.Sleep(50 * time.Millisecond)
	e.mu.Lock()
	defer e.mu.Unlock()
	var changes []string
	for _, op := range e.ops {
		if op == "on" || op == "off" {
			changes = append(changes, op)
		}
	}
	if len(changes) != 2 || changes[0] != "on" || changes[1] != "off" || e.running {
		t.Fatalf("got %v, running %v", changes, e.running)
	}
}
