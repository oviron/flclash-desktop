package app

import (
	"encoding/json"
	"os"

	"github.com/oviron/flclash-desktop/internal/say"
	"github.com/oviron/flclash-desktop/internal/store"
)

// backup is everything the user has set up, in one document. It holds the
// credentials of the servers and the addresses of the subscriptions.
type backup struct {
	// Format is the version of this document.
	Format   int               `json:"flclashBackup"`
	State    store.State       `json:"state"`
	Policies map[string]string `json:"policies"`
}

// WriteBackup saves everything the user has set up into a file only the
// user can read.
func WriteBackup(st *store.Store, path string) error {
	state, err := st.Load()
	if err != nil {
		return err
	}
	doc := backup{Format: 1, State: state, Policies: map[string]string{}}
	names, err := st.PolicyNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, isBuiltin := st.Builtin(name); isBuiltin {
			continue
		}
		text, err := st.PolicyText(name)
		if err != nil {
			return err
		}
		doc.Policies[name] = text
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// ReadBackup puts a saved setup in place of the current one. Nothing may run
// meanwhile, and a backup that does not read whole changes nothing.
func ReadBackup(st *store.Store, path string) error {
	if status, err := Status(); err == nil && (status.Running || status.Blocked) {
		return say.New("turn FlClash off first: a restore replaces the server that runs")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > 16<<20 {
		return say.New("the file is too large to be a backup")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc backup
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Format == 0 {
		return say.New("the file is not a FlClash backup")
	}
	if doc.Format != 1 {
		return say.Errorf("the backup is of format %d, which this version does not read", doc.Format)
	}
	// Everything is checked before anything is written.
	for _, entry := range doc.State.Servers {
		if err := checkStored(entry); err != nil {
			return say.Errorf("the backup holds a server that cannot be used: %w", err)
		}
	}
	for name, text := range doc.Policies {
		if err := st.CheckPolicy(name, text); err != nil {
			return say.Errorf("the backup holds a policy that cannot be used: %w", err)
		}
	}
	current, err := st.PolicyNames()
	if err != nil {
		return err
	}
	for _, name := range current {
		if _, isBuiltin := st.Builtin(name); !isBuiltin {
			if err := st.RemovePolicy(name); err != nil {
				return err
			}
		}
	}
	for name, text := range doc.Policies {
		if err := st.ImportPolicy(name, text); err != nil {
			return err
		}
	}
	if _, err := st.Policy(doc.State.Policy); err != nil {
		doc.State.Policy = "all"
	}
	return st.Save(doc.State)
}
