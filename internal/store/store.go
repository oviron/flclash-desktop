// Package store keeps the user's servers, policies and current choice on disk.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/oviron/flclash-desktop/internal/ingest"
	"github.com/oviron/flclash-desktop/internal/policy"
)

// Entry is a stored server. The ID stays the same when the server is imported
// again or renamed, so a selection survives a subscription update.
type Entry struct {
	ID    string `json:"id"`
	Group string `json:"group"`
	ingest.Server
}

// State is the whole of what the user has set up.
type State struct {
	Servers  []Entry `json:"servers"`
	Selected string  `json:"selected,omitempty"` // server ID
	Policy   string  `json:"policy,omitempty"`   // policy name
}

// Store is a directory holding state.json and policies/.
type Store struct{ dir string }

// Open uses $FLCLASH_HOME, or the per-user config directory of the system.
func Open() (*Store, error) {
	dir := os.Getenv("FLCLASH_HOME")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(base, "flclash-desktop")
	}
	// Server entries carry credentials: the directory is the user's alone.
	if err := os.MkdirAll(filepath.Join(dir, "policies"), 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// CoreDir is where the core keeps its own files.
func (s *Store) CoreDir() string { return filepath.Join(s.dir, "core") }

func (s *Store) statePath() string { return filepath.Join(s.dir, "state.json") }

// Load returns the saved state; a store never written to is empty, with the
// first built-in policy chosen.
func (s *Store) Load() (State, error) {
	state := State{Policy: policy.BuiltinNames()[0]}
	raw, err := os.ReadFile(s.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, fmt.Errorf("%s is damaged: %w", s.statePath(), err)
	}
	return state, nil
}

// Save replaces the state in one step: a crash leaves the old file or the new.
func (s *Store) Save(state State) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.statePath(), raw)
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Put adds a server to a group, or replaces the one with the same name there
// and keeps its ID.
func (state *State) Put(group string, server ingest.Server) Entry {
	for i, e := range state.Servers {
		if e.Group == group && e.Name == server.Name {
			state.Servers[i].Server = server
			return state.Servers[i]
		}
	}
	entry := Entry{ID: newID(), Group: group, Server: server}
	state.Servers = append(state.Servers, entry)
	return entry
}

func newID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// Find takes an ID or a name; a name several groups share is ambiguous.
func (state State) Find(ref string) (Entry, error) {
	var byName []Entry
	for _, e := range state.Servers {
		if e.ID == ref {
			return e, nil
		}
		if e.Name == ref {
			byName = append(byName, e)
		}
	}
	switch len(byName) {
	case 0:
		return Entry{}, fmt.Errorf("no server %q", ref)
	case 1:
		return byName[0], nil
	}
	ids := make([]string, len(byName))
	for i, e := range byName {
		ids[i] = e.ID + " (" + e.Group + ")"
	}
	return Entry{}, fmt.Errorf("several servers are named %q, pick one by ID: %s", ref, strings.Join(ids, ", "))
}

var policyName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func (s *Store) policyPath(name string) string {
	return filepath.Join(s.dir, "policies", name+".rules")
}

// ImportPolicy checks a policy document and stores it under a name.
func (s *Store) ImportPolicy(name, text string) error {
	if !policyName.MatchString(name) {
		return fmt.Errorf("policy name %q: use lower-case letters, digits, - and _", name)
	}
	if _, isBuiltin := policy.Builtin(name); isBuiltin {
		return fmt.Errorf("%q is a built-in policy and cannot be replaced", name)
	}
	if _, err := policy.Parse(name, text); err != nil {
		return err
	}
	return writeAtomic(s.policyPath(name), []byte(text))
}

// RemovePolicy deletes an imported policy; the built-in ones stay.
func (s *Store) RemovePolicy(name string) error {
	if _, isBuiltin := policy.Builtin(name); isBuiltin {
		return fmt.Errorf("%q is a built-in policy and cannot be removed", name)
	}
	if !policyName.MatchString(name) {
		return fmt.Errorf("no policy %q", name)
	}
	err := os.Remove(s.policyPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no policy %q", name)
	}
	return err
}

// Policy returns a built-in or an imported policy.
func (s *Store) Policy(name string) (policy.Policy, error) {
	if p, ok := policy.Builtin(name); ok {
		return p, nil
	}
	if !policyName.MatchString(name) {
		return policy.Policy{}, fmt.Errorf("no policy %q", name)
	}
	raw, err := os.ReadFile(s.policyPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return policy.Policy{}, fmt.Errorf("no policy %q", name)
	}
	if err != nil {
		return policy.Policy{}, err
	}
	return policy.Parse(name, string(raw))
}

// PolicyNames lists the built-in policies, then the imported ones by name.
func (s *Store) PolicyNames() ([]string, error) {
	names := policy.BuiltinNames()
	files, err := os.ReadDir(filepath.Join(s.dir, "policies"))
	if err != nil {
		return nil, err
	}
	var imported []string
	for _, f := range files {
		if name, ok := strings.CutSuffix(f.Name(), ".rules"); ok {
			imported = append(imported, name)
		}
	}
	sort.Strings(imported)
	return append(names, imported...), nil
}
