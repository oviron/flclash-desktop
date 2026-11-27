package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/oviron/flclash-desktop/internal/helper"
)

// isolated gives the engine its own store and no helper to reach.
func isolated(t *testing.T) {
	t.Helper()
	t.Setenv("FLCLASH_HOME", t.TempDir())
	if runtime.GOOS == "windows" {
		t.Setenv("FLCLASH_HELPER_SOCK", fmt.Sprintf(`\\.\pipe\flclash-test-none-%d`, time.Now().UnixNano()))
		return
	}
	dir, err := os.MkdirTemp("", "fl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("FLCLASH_HELPER_SOCK", filepath.Join(dir, "none.sock"))
}

func handle(t *testing.T, req Request) Reply {
	t.Helper()
	r := Handle(req)
	if r.Error != "" {
		t.Fatalf("%s: %s", req.Op, r.Error)
	}
	return r
}

const links = "vless://11111111-2222-3333-4444-555555555555@a.example:443?type=tcp&security=tls&sni=a.example&fp=chrome#first\n" +
	"vless://11111111-2222-3333-4444-555555555555@b.example:443?type=ws&security=tls&sni=b.example&path=%2Fws&host=b.example#second"

// What a window does first: import, choose, change the mode and the port,
// and read the state back the way the window shows it.
func TestWindowFlow(t *testing.T) {
	isolated(t)
	r := handle(t, Request{Op: "import", Text: links})
	if len(r.Imported.Imported) != 2 || len(r.Imported.Failed) != 0 {
		t.Fatalf("imported %+v", r.Imported)
	}
	if r.View == nil || len(r.View.Servers) != 2 {
		t.Fatalf("view after import: %+v", r.View)
	}
	second := r.View.Servers[1]
	if second.Name != "second" || second.Group != "default" || second.Protocol == "" {
		t.Fatalf("server %+v", second)
	}

	if v := handle(t, Request{Op: "select", Server: second.ID}).View; v.Selected != second.ID {
		t.Fatalf("selected %q, want %q", v.Selected, second.ID)
	}
	if v := handle(t, Request{Op: "mode", Text: helper.ModeSystem}).View; v.Mode != helper.ModeSystem {
		t.Fatalf("mode %q", v.Mode)
	}
	if v := handle(t, Request{Op: "port", Text: "17891"}).View; v.Port != 17891 {
		t.Fatalf("port %d", v.Port)
	}
	if link := handle(t, Request{Op: "link", Server: second.ID}).Link; !strings.HasPrefix(link, "vless://") || !strings.Contains(link, "b.example") {
		t.Fatalf("link %q", link)
	}
	if v := handle(t, Request{Op: "remove", Server: second.ID}).View; len(v.Servers) != 1 {
		t.Fatalf("servers after remove: %+v", v.Servers)
	}
}

// Without a helper the status is a state to show, not a failure: nothing runs.
func TestStatusWithoutHelper(t *testing.T) {
	isolated(t)
	s := handle(t, Request{Op: "status"}).Status
	if s == nil || s.Installed || s.Running {
		t.Fatalf("status %+v", s)
	}
}

// What the engine refuses comes back as an error, in the language asked for.
func TestRefusals(t *testing.T) {
	isolated(t)
	if r := Handle(Request{Op: "nonsense"}); !strings.Contains(r.Error, "nonsense") {
		t.Fatalf("unknown op: %+v", r)
	}
	if r := Handle(Request{Op: "port", Text: "80"}); r.Error == "" {
		t.Fatal("a privileged port was taken")
	}
	r := Handle(Request{Op: "import", Text: "vless://11111111-2222-3333-4444-555555555555@a.example:443?type=tcp&security=tls&unknownfield=1#odd"})
	if r.Imported == nil || len(r.Imported.Imported) != 0 || len(r.Imported.Failed) != 1 {
		t.Fatalf("a link with an unknown field: %+v", r)
	}
}
