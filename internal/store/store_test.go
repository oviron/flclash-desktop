package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/oviron/flclash-desktop/internal/ingest"
)

func open(t *testing.T) *Store {
	t.Helper()
	t.Setenv("FLCLASH_HOME", t.TempDir())
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func server(name, host string) ingest.Server {
	return ingest.Server{Name: name, Proxy: map[string]any{"type": "hysteria2", "server": host}}
}

// A selection points at an ID, so importing a server again must not change it.
func TestReimportKeepsTheID(t *testing.T) {
	s := open(t)
	state, _ := s.Load()
	first := state.Put("default", server("nl-hy2", "old.example"))
	state.Selected = first.ID
	again := state.Put("default", server("nl-hy2", "new.example"))
	if again.ID != first.ID || len(state.Servers) != 1 || again.Proxy["server"] != "new.example" {
		t.Fatalf("re-import made %#v out of %#v", again, first)
	}
	other := state.Put("paid", server("nl-hy2", "paid.example"))
	if other.ID == first.ID {
		t.Fatal("the same name in another group is another server")
	}
	if _, err := state.Find("nl-hy2"); err == nil || !strings.Contains(err.Error(), "several servers") {
		t.Fatalf("an ambiguous name must be refused, got %v", err)
	}
	if found, err := state.Find(first.ID); err != nil || found.Group != "default" {
		t.Fatalf("got %#v, %v", found, err)
	}
	if err := s.Save(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load()
	if err != nil || loaded.Selected != first.ID || len(loaded.Servers) != 2 {
		t.Fatalf("got %#v, %v", loaded, err)
	}
	info, err := os.Stat(filepath.Join(os.Getenv("FLCLASH_HOME"), "state.json"))
	// On Windows the profile's access list keeps the file private; the mode
	// bits say nothing there.
	if runtime.GOOS != "windows" && (err != nil || info.Mode().Perm() != 0o600) {
		t.Fatalf("state.json holds credentials and must be 0600, got %v %v", info.Mode(), err)
	}
}

func TestEmptyStoreStartsWithABuiltinPolicy(t *testing.T) {
	state, err := open(t).Load()
	if err != nil || state.Policy != "all" || len(state.Servers) != 0 {
		t.Fatalf("got %#v, %v", state, err)
	}
}

func TestPolicyImport(t *testing.T) {
	s := open(t)
	if err := s.ImportPolicy("work", "DOMAIN-SUFFIX,corp.example,DIRECT\nMATCH,PROXY\n"); err != nil {
		t.Fatal(err)
	}
	p, err := s.Policy("work")
	if err != nil || len(p.Rules) != 2 {
		t.Fatalf("got %#v, %v", p, err)
	}
	names, _ := s.PolicyNames()
	if strings.Join(names, ",") != "all,lan-direct,ru-direct,work" {
		t.Fatalf("got %v", names)
	}
	for name, text := range map[string]string{
		"all":      "MATCH,PROXY\n",            // a built-in is not replaced
		"../x":     "MATCH,PROXY\n",            // not a path
		"broken":   "DOMAIN,a.example,PROXY\n", // no MATCH: nothing is stored
		"wrongdst": "MATCH,somewhere\n",
	} {
		if err := s.ImportPolicy(name, text); err == nil {
			t.Errorf("policy %q must be refused", name)
		}
	}
	if _, err := s.Policy("broken"); err == nil {
		t.Fatal("a refused policy must not be stored")
	}
	if err := s.RemovePolicy("all"); err == nil {
		t.Fatal("a built-in policy must not be removed")
	}
	if err := s.RemovePolicy("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Policy("work"); err == nil {
		t.Fatal("a removed policy must be gone")
	}
}

// A copy with the portable mark keeps its data beside it; one without does not.
func TestPortableDir(t *testing.T) {
	program := t.TempDir()
	if _, ok := portableDir(program); ok {
		t.Fatal("a copy without the mark counted as portable")
	}
	if err := os.Mkdir(filepath.Join(program, PortableMark), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, ok := portableDir(program); ok {
		t.Fatal("a folder named like the mark counted as the mark")
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, PortableMark), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if dir, ok := portableDir(other); !ok || dir != filepath.Join(other, PortableData) {
		t.Fatalf("portable data at %q, %v", dir, ok)
	}
}
