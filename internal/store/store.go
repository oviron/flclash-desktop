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
	"time"

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
	// Subscriptions are the groups that are filled from an address.
	Subscriptions []Subscription `json:"subscriptions,omitempty"`
	// Mode is how the host is served: "tun", "system" or "proxy"; empty is
	// "proxy", the one that changes nothing in the system.
	Mode string `json:"mode,omitempty"`
	// Port is the loopback port for HTTP and SOCKS; zero is DefaultPort.
	Port int `json:"port,omitempty"`
	// LAN opens the local port to the local network; Auth is the
	// user:password it then asks of other devices; empty asks nothing.
	LAN  bool   `json:"lan,omitempty"`
	Auth string `json:"auth,omitempty"`
	// DNS resolves names under the tunnel; empty is the default resolver.
	DNS string `json:"dns,omitempty"`
	// AutoConnect: the app turns the tunnel on when it starts.
	AutoConnect bool `json:"autoConnect,omitempty"`
	// Unprotected turns the full guard off: traffic may then leave past the
	// tunnel, and does when the core dies. Protection is what a new setup gets.
	Unprotected bool `json:"unprotected,omitempty"`
}

// Subscription is a group of servers that comes from an address and is read
// from it again from time to time. Its servers are the entries whose Group is
// its Name.
type Subscription struct {
	Name string `json:"name"`
	// URL usually carries the token of the account: it is a secret.
	URL     string    `json:"url"`
	Updated time.Time `json:"updated,omitempty"`
	// Hours is how often the provider asked to be read; zero means it did not say.
	Hours int `json:"hours,omitempty"`
	// What the provider said about the account: bytes used and allowed, and
	// when it ends, as unix seconds. Zero means it did not say.
	Used   int64 `json:"used,omitempty"`
	Total  int64 `json:"total,omitempty"`
	Expire int64 `json:"expire,omitempty"`
	// Problem is why the last update failed; empty after one that worked.
	Problem string `json:"problem,omitempty"`
	// Agent is the client name the provider is told; empty is this app's own.
	Agent string `json:"agent,omitempty"`
	// Every is how often to read it, in hours, when the user says so; zero
	// leaves it to the provider.
	Every int `json:"every,omitempty"`
	// Skip leaves out the servers whose name has any of these words in it,
	// written with commas between them.
	Skip string `json:"skip,omitempty"`
}

// DefaultPort is the local port of a new setup, the one Clash clients use.
const DefaultPort = 7890

// LocalPort is the port in effect.
func (state State) LocalPort() int {
	if state.Port == 0 {
		return DefaultPort
	}
	return state.Port
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

// Builtin says whether a name is that of a built-in policy.
func (s *Store) Builtin(name string) (policy.Policy, bool) { return policy.Builtin(name) }

// CheckPolicy says whether a document could be stored under a name.
func (s *Store) CheckPolicy(name, text string) error {
	if !policyName.MatchString(name) {
		return fmt.Errorf("policy name %q: use lower-case letters, digits, - and _", name)
	}
	if _, isBuiltin := policy.Builtin(name); isBuiltin {
		return fmt.Errorf("%q is a built-in policy and cannot be replaced", name)
	}
	_, err := policy.Parse(name, text)
	return err
}

// ImportPolicy checks a policy document and stores it under a name.
func (s *Store) ImportPolicy(name, text string) error {
	if err := s.CheckPolicy(name, text); err != nil {
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

// PolicyText returns the document of a policy as it was written.
func (s *Store) PolicyText(name string) (string, error) {
	if text, ok := policy.BuiltinText(name); ok {
		return text, nil
	}
	if !policyName.MatchString(name) {
		return "", fmt.Errorf("no policy %q", name)
	}
	raw, err := os.ReadFile(s.policyPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no policy %q", name)
	}
	return string(raw), err
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
