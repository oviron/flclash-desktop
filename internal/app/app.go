// Package app is what the product does, for whichever front end asks: the
// command line and the windows call the same functions.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oviron/flclash-desktop/internal/compile"

	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/ingest"
	"github.com/oviron/flclash-desktop/internal/policy"
	"github.com/oviron/flclash-desktop/internal/store"
)

// Server is a stored server as a front end shows it: no credentials.
type Server struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Group    string `json:"group"`
	Protocol string `json:"protocol"`
}

// Policy is a routing policy as a front end shows it.
type Policy struct {
	Name    string `json:"name"`
	Builtin bool   `json:"builtin"`
	// Rules counts the rules; zero for a stored policy that no longer reads.
	Rules int `json:"rules"`
}

// View is everything a front end lists.
type View struct {
	Servers  []Server `json:"servers"`
	Selected string   `json:"selected"`
	Policies []Policy `json:"policies"`
	Policy   string   `json:"policy"`
}

// Snapshot reads the current state.
func Snapshot(st *store.Store) (View, error) {
	state, err := st.Load()
	if err != nil {
		return View{}, err
	}
	names, err := st.PolicyNames()
	if err != nil {
		return View{}, err
	}
	view := View{Servers: []Server{}, Selected: state.Selected, Policies: []Policy{}, Policy: state.Policy}
	for _, name := range names {
		_, isBuiltin := policy.Builtin(name)
		p, _ := st.Policy(name)
		view.Policies = append(view.Policies, Policy{Name: name, Builtin: isBuiltin, Rules: len(p.Rules)})
	}
	for _, e := range state.Servers {
		view.Servers = append(view.Servers, Server{ID: e.ID, Name: e.Name, Group: e.Group, Protocol: protocol(e.Proxy)})
	}
	return view, nil
}

// protocol names a server the way people call it: "VLESS REALITY XHTTP".
func protocol(p map[string]any) string {
	kind, _ := p["type"].(string)
	switch kind {
	case "hysteria2":
		return "Hysteria2"
	case "tuic":
		return "TUIC"
	case "vless":
		parts := []string{"VLESS"}
		if _, reality := p["reality-opts"]; reality {
			parts = append(parts, "REALITY")
		} else if _, encrypted := p["encryption"]; encrypted {
			parts = append(parts, "Encryption")
		}
		switch network, _ := p["network"].(string); network {
		case "xhttp":
			parts = append(parts, "XHTTP")
		case "grpc":
			parts = append(parts, "gRPC")
		}
		if flow, _ := p["flow"].(string); strings.HasSuffix(flow, "vision") {
			parts = append(parts, "Vision")
		}
		return strings.Join(parts, " ")
	}
	return kind
}

// Failure is one entry of an import that was refused.
type Failure struct {
	Entry  int    `json:"entry"` // 1-based position among the non-empty lines
	Reason string `json:"reason"`
}

// ImportResult says what an import did. A refused entry does not stop the rest.
type ImportResult struct {
	Imported []Server  `json:"imported"`
	Failed   []Failure `json:"failed"`
}

// Import adds the servers written in text, one per line, to a group. An entry
// with the name of a server already in the group replaces it and keeps its ID.
func Import(st *store.Store, group, text string) (ImportResult, error) {
	result := ImportResult{Imported: []Server{}, Failed: []Failure{}}
	state, err := st.Load()
	if err != nil {
		return result, err
	}
	n := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n++
		server, err := ingest.FromThrone(line)
		if err != nil {
			result.Failed = append(result.Failed, Failure{Entry: n, Reason: err.Error()})
			continue
		}
		e := state.Put(group, server)
		result.Imported = append(result.Imported, Server{ID: e.ID, Name: e.Name, Group: e.Group, Protocol: protocol(e.Proxy)})
	}
	if n == 0 {
		return result, errors.New("there is nothing to import")
	}
	return result, st.Save(state)
}

// Select saves a new choice of server, policy or both; an empty one stays as
// it is. A tunnel that is up moves to the choice.
func Select(st *store.Store, serverRef, policyName string) error {
	state, err := st.Load()
	if err != nil {
		return err
	}
	if serverRef != "" {
		entry, err := state.Find(serverRef)
		if err != nil {
			return err
		}
		state.Selected = entry.ID
	}
	if policyName != "" {
		if _, err := st.Policy(policyName); err != nil {
			return err
		}
		state.Policy = policyName
	}
	if err := st.Save(state); err != nil {
		return err
	}
	if status, err := Status(); err == nil && status.Running {
		_, err = On(st)
		return err
	}
	return nil
}

// Remove deletes a server. The selected one cannot be removed while the
// tunnel runs through it.
func Remove(st *store.Store, id string) error {
	state, err := st.Load()
	if err != nil {
		return err
	}
	kept := state.Servers[:0]
	found := false
	for _, e := range state.Servers {
		if e.ID == id {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		return errors.New("no such server")
	}
	if state.Selected == id {
		if status, err := Status(); err == nil && status.Running {
			return errors.New("the tunnel runs through this server: turn it off or pick another first")
		}
		state.Selected = ""
	}
	state.Servers = kept
	return st.Save(state)
}

// On turns the tunnel on with the current choice, or moves a running tunnel
// to it. The status comes back also with an error: what is still true matters
// most when a change did not go through.
func On(st *store.Store) (helper.Status, error) {
	state, err := st.Load()
	if err != nil {
		return helper.Status{}, err
	}
	if state.Selected == "" {
		return helper.Status{}, errors.New("no server is selected")
	}
	entry, err := state.Find(state.Selected)
	if err != nil {
		return helper.Status{}, err
	}
	pol, err := st.Policy(state.Policy)
	if err != nil {
		return helper.Status{}, err
	}
	return helper.Call(helper.Request{
		Op:       helper.OpApply,
		Revision: time.Now().UnixNano(),
		Server:   entry.Server,
		Policy:   pol.Name,
		Rules:    pol.Rules,
	}, 90*time.Second)
}

// Off turns the tunnel off.
func Off() (helper.Status, error) {
	return helper.Call(helper.Request{Op: helper.OpStop}, 30*time.Second)
}

// Status asks the helper what is true right now.
func Status() (helper.Status, error) {
	return helper.Call(helper.Request{Op: helper.OpStatus}, 10*time.Second)
}

// RemovePolicy deletes an imported policy. The one in use stays until another
// is chosen, so the choice never points at nothing.
func RemovePolicy(st *store.Store, name string) error {
	state, err := st.Load()
	if err != nil {
		return err
	}
	if state.Policy == name {
		return errors.New("this policy is in use: choose another first")
	}
	return st.RemovePolicy(name)
}

// Delay measures one server: a new connection through it and one small
// request, in milliseconds. A running tunnel measures from inside its core;
// otherwise a core is run for the moment, without privileges.
func Delay(st *store.Store, ref string) (int, error) {
	state, err := st.Load()
	if err != nil {
		return 0, err
	}
	entry, err := state.Find(ref)
	if err != nil {
		return 0, err
	}
	if err := ingest.Check(entry.Server); err != nil {
		return 0, err
	}
	if status, err := Status(); err == nil && status.Running {
		return helper.Delay(entry.Server)
	}
	return localDelay(entry.Server)
}

// localDelay runs the core binary that ships next to this program.
func localDelay(server ingest.Server) (int, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, err
	}
	request, err := json.Marshal(helper.CoreDelay{
		Proxy:     compile.Proxy(server),
		URL:       helper.DelayURL,
		TimeoutMS: int(helper.DelayTimeout / time.Millisecond),
	})
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), helper.DelayTimeout+5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(self), "flclash-core"), "-delay")
	// The server goes in through stdin: its credentials stay out of the process list.
	cmd.Stdin = bytes.NewReader(request)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("the core did not run: %w", err)
	}
	var reply helper.CoreReply
	if err := json.Unmarshal(out, &reply); err != nil {
		return 0, fmt.Errorf("the core answered nonsense: %w", err)
	}
	if reply.Error != "" {
		return 0, errors.New(reply.Error)
	}
	return reply.Delay, nil
}
