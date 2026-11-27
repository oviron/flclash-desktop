//go:build !windows

package helper

import (
	"path/filepath"
	"testing"
)

// With no helper installed the app runs the proxy modes itself, and only the
// tunnel asks for the helper.
func TestLocalRunsTheProxyModes(t *testing.T) {
	t.Setenv("FLCLASH_HELPER_SOCK", filepath.Join(t.TempDir(), "none.sock"))
	s := supervisor(t, false)
	s.HomeDir = t.TempDir()
	s.Listening = func(int) bool { return true }
	s.PortFree = func(int) bool { return true }
	s.init()
	local.mu.Lock()
	local.s = s
	local.mu.Unlock()
	t.Cleanup(func() {
		local.mu.Lock()
		local.s = nil
		local.mu.Unlock()
	})

	if h := Probe(); h.Installed || h.Problem != nil {
		t.Fatalf("a missing helper probed as %+v", h)
	}
	// A missing helper is no failure: nothing runs.
	if st, err := Call(Request{Op: OpStatus}, 0); err != nil || st.Running {
		t.Fatalf("status without a helper: %+v, %v", st, err)
	}

	proxy := request("first", 1)
	proxy.Mode, proxy.Port = ModeProxy, 17890
	if _, err := Call(proxy, 0); err != nil {
		t.Fatalf("the port did not open without a helper: %v", err)
	}
	if st, err := Call(Request{Op: OpStatus}, 0); err != nil || !st.Running || st.Mode != ModeProxy || st.Server != "first" {
		t.Fatalf("status of the app's own core: %+v, %v", st, err)
	}
	if d, err := Delay(proxy.Server); err != nil || d != 42 {
		t.Fatalf("a delay went past the running core: %d, %v", d, err)
	}

	// The tunnel goes to the helper; the port is let go of first.
	tunnel := request("first", 2)
	if _, err := Call(tunnel, 0); !IsNotInstalled(err) {
		t.Fatalf("the tunnel without a helper: %v", err)
	}
	if s.Status().Running {
		t.Fatal("the app's core still runs beside a tunnel")
	}

	if _, err := Call(proxy, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Call(Request{Op: OpStop}, 0); err != nil {
		t.Fatalf("stop without a helper: %v", err)
	}
	if s.Status().Running {
		t.Fatal("stop left the app's core running")
	}
}
