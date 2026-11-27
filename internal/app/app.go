// Package app is what the product does, for whichever front end asks: the
// command line and the windows call the same functions.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oviron/flclash-desktop/internal/compile"

	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/ingest"
	"github.com/oviron/flclash-desktop/internal/policy"
	"github.com/oviron/flclash-desktop/internal/say"
	"github.com/oviron/flclash-desktop/internal/store"
)

// Server is a stored server as a front end shows it: no credentials.
type Server struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Group    string `json:"group"`
	Protocol string `json:"protocol"`
	// Country is where the server lets traffic out, once that was asked.
	Country string `json:"country,omitempty"`
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
	// Protect: nothing leaves past the tunnel, also when the core dies.
	Protect bool `json:"protect"`
	// Mode is how the host is served: "tun", "system" or "proxy".
	Mode string `json:"mode"`
	// Port is the loopback port for HTTP and SOCKS.
	Port int `json:"port"`
	// AutoConnect: the app turns the tunnel on when it starts.
	AutoConnect bool `json:"autoConnect"`
	// Subscriptions are the groups that are filled from an address.
	Subscriptions []Subscription `json:"subscriptions"`
	// LAN: other devices of the local network may use the local port; Auth
	// is the user:password asked of them, empty for none.
	LAN  bool   `json:"lan"`
	Auth string `json:"auth"`
	// DNS resolves names under the tunnel.
	DNS string `json:"dns"`
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
	view := View{Servers: []Server{}, Selected: state.Selected, Policies: []Policy{}, Policy: state.Policy, Protect: !state.Unprotected, Mode: mode(state), Port: state.LocalPort(), AutoConnect: state.AutoConnect, Subscriptions: subscriptions(state),
		LAN: state.LAN, Auth: state.Auth, DNS: dns(state)}
	for _, name := range names {
		_, isBuiltin := policy.Builtin(name)
		p, _ := st.Policy(name)
		view.Policies = append(view.Policies, Policy{Name: name, Builtin: isBuiltin, Rules: len(p.Rules)})
	}
	for _, e := range state.Servers {
		view.Servers = append(view.Servers, Server{ID: e.ID, Name: e.Name, Group: e.Group, Protocol: protocol(e.Proxy), Country: e.Country})
	}
	return view, nil
}

// protocol names a server the way people call it: "VLESS REALITY XHTTP".
func protocol(p map[string]any) string {
	label := protocolName(p)
	// A server whose certificate is not checked can be impersonated by anyone
	// on the way to it; the list says so.
	if insecure, _ := p["skip-cert-verify"].(bool); insecure {
		label += ", certificate not checked"
	}
	return label
}

func protocolName(p map[string]any) string {
	kind, _ := p["type"].(string)
	switch kind {
	case "hysteria2":
		return "Hysteria2"
	case "tuic":
		return "TUIC"
	case "ss":
		return "Shadowsocks"
	case "trojan", "vmess":
		parts := []string{map[string]string{"trojan": "Trojan", "vmess": "VMess"}[kind]}
		switch network, _ := p["network"].(string); network {
		case "ws":
			parts = append(parts, "WebSocket")
		case "grpc":
			parts = append(parts, "gRPC")
		}
		return strings.Join(parts, " ")
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
		case "ws":
			parts = append(parts, "WebSocket")
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

// Import adds the servers written in text to a group: links or Throne
// profiles one per line, or a Clash config. An entry
// with the name of a server already in the group replaces it and keeps its ID.
func Import(st *store.Store, group, text string) (ImportResult, error) {
	result := ImportResult{Imported: []Server{}, Failed: []Failure{}}
	state, err := st.Load()
	if err != nil {
		return result, err
	}
	servers, refused, n := ingest.ParseAll(text)
	for _, r := range refused {
		result.Failed = append(result.Failed, Failure{Entry: r.Entry, Reason: r.Reason})
	}
	for _, server := range servers {
		e := state.Put(group, server)
		result.Imported = append(result.Imported, Server{ID: e.ID, Name: e.Name, Group: e.Group, Protocol: protocol(e.Proxy)})
	}
	if n == 0 {
		return result, say.New("there is nothing to import")
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
		return say.New("no such server")
	}
	if state.Selected == id {
		if status, err := Status(); err == nil && status.Running {
			return say.New("the tunnel runs through this server: turn it off or pick another first")
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
		return helper.Status{}, say.New("no server is selected")
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
		Protect:  !state.Unprotected,
		Mode:     mode(state),
		Port:     state.LocalPort(),
		LAN:      state.LAN,
		Auth:     state.Auth,
		DNS:      state.DNS,
	}, 90*time.Second)
}

// mode is the mode in effect. A new setup opens the local port and nothing
// else: the tunnel and the system proxy change how the whole machine behaves,
// so they are on only for someone who chose them.
func mode(state store.State) string {
	if state.Mode == "" {
		return helper.ModeProxy
	}
	return state.Mode
}

// change saves an edit of the settings; what runs takes it at once.
func change(st *store.Store, edit func(*store.State) error) error {
	state, err := st.Load()
	if err != nil {
		return err
	}
	if err := edit(&state); err != nil {
		return err
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

// SetMode chooses how the host is served.
func SetMode(st *store.Store, name string) error {
	return change(st, func(state *store.State) error {
		switch name {
		case helper.ModeTUN, helper.ModeSystem, helper.ModeProxy:
			state.Mode = name
			return nil
		}
		return say.Errorf("unknown mode %q: tun, system or proxy", name)
	})
}

// SetAutoConnect saves whether the app connects when it starts. Nothing that
// runs changes.
func SetAutoConnect(st *store.Store, on bool) error {
	state, err := st.Load()
	if err != nil {
		return err
	}
	state.AutoConnect = on
	return st.Save(state)
}

func dns(state store.State) string {
	if state.DNS == "" {
		return compile.DefaultDNS
	}
	return state.DNS
}

// SetLAN opens the local port to the local network, or closes it.
func SetLAN(st *store.Store, on bool) error {
	return change(st, func(state *store.State) error {
		state.LAN = on
		return nil
	})
}

// SetAuth sets the user:password other devices give at the local port; an
// empty one asks for none.
func SetAuth(st *store.Store, auth string) error {
	auth = strings.TrimSpace(auth)
	return change(st, func(state *store.State) error {
		if auth != "" {
			if err := compile.CheckAuth(auth); err != nil {
				return err
			}
		}
		state.Auth = auth
		return nil
	})
}

// SetDNS sets the resolver used under the tunnel; an empty one, or the
// default one, goes back to the default.
func SetDNS(st *store.Store, address string) error {
	address = strings.TrimSpace(address)
	return change(st, func(state *store.State) error {
		if address == "" || address == compile.DefaultDNS {
			state.DNS = ""
			return nil
		}
		if err := compile.CheckDNS(address); err != nil {
			return err
		}
		state.DNS = address
		return nil
	})
}

// SetPort chooses the local port.
func SetPort(st *store.Store, port int) error {
	return change(st, func(state *store.State) error {
		if port < 1024 || port > 65535 {
			return say.Errorf("the local port must be between 1024 and 65535, not %d", port)
		}
		state.Port = port
		return nil
	})
}

// Protect saves whether traffic may leave past the tunnel. A tunnel that is
// up takes the new setting at once.
func Protect(st *store.Store, on bool) error {
	return change(st, func(state *store.State) error {
		state.Unprotected = !on
		return nil
	})
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
		return say.New("this policy is in use: choose another first")
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
	status, err := Status()
	running := err == nil && status.Running
	var ms int
	if running {
		ms, err = helper.Delay(entry.Server)
	} else {
		ms, err = runCore("-delay", entry.Server, helper.DelayURL)
	}
	if err == nil && time.Since(time.Unix(entry.Asked, 0)) > countryAge(entry) {
		askCountry(st, entry, running)
	}
	return ms, err
}

// countryAge is how long the country of a server is taken as known. A server
// that answers is asked about its country this seldom, so that a measurement
// costs the server one more small request at most once a week.
func countryAge(entry store.Entry) time.Duration {
	if entry.Country == "" {
		return 24 * time.Hour
	}
	return 7 * 24 * time.Hour
}

// askCountry asks through a server where it lets traffic out and keeps the
// answer. No answer keeps what was known.
func askCountry(st *store.Store, entry store.Entry, running bool) {
	var exit helper.Exit
	var err error
	if running {
		exit, err = helper.ExitOf(entry.Server)
	} else {
		var reply helper.CoreReply
		reply, err = callCore("-exit", entry.Server, helper.ExitURL)
		if reply.Exit != nil {
			exit = *reply.Exit
		}
	}
	RememberCountry(st, entry.ID, exit.Country, err == nil)
}

// RememberCountry keeps the country a server was found to let traffic out
// in; a failed ask only says when it was made.
func RememberCountry(st *store.Store, id, country string, answered bool) {
	state, err := st.Load()
	if err != nil {
		return
	}
	for i := range state.Servers {
		if state.Servers[i].ID == id {
			state.Servers[i].Asked = time.Now().Unix()
			if answered && country != "" {
				state.Servers[i].Country = country
			}
			_ = st.Save(state)
			return
		}
	}
}

// runCore measures the delay of a server with the core binary that ships
// next to this program.
func runCore(mode string, server ingest.Server, url string) (int, error) {
	reply, err := callCore(mode, server, url)
	return reply.Delay, err
}

// callCore runs the core for one request, without privileges.
func callCore(mode string, server ingest.Server, url string) (helper.CoreReply, error) {
	self, err := os.Executable()
	if err != nil {
		return helper.CoreReply{}, err
	}
	request, err := json.Marshal(helper.CoreDelay{
		Proxy:     compile.Proxy(server),
		URL:       url,
		TimeoutMS: int(helper.DelayTimeout / time.Millisecond),
		Lang:      say.Language(),
	})
	if err != nil {
		return helper.CoreReply{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), helper.DelayTimeout+5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(self), CoreName), mode)
	hide(cmd)
	// The server goes in through stdin: its credentials stay out of the process list.
	cmd.Stdin = bytes.NewReader(request)
	out, err := cmd.Output()
	if err != nil {
		return helper.CoreReply{}, say.Errorf("the core did not run: %w", err)
	}
	var reply helper.CoreReply
	if err := json.Unmarshal(out, &reply); err != nil {
		return helper.CoreReply{}, say.Errorf("the core answered nonsense: %w", err)
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	return reply, nil
}

// checkStored says whether a stored server is one the helper would run.
func checkStored(entry store.Entry) error {
	if entry.ID == "" {
		return say.New("a server has no ID")
	}
	return ingest.Check(entry.Server)
}

// Link writes a stored server as a share link. The link carries the
// credentials of the server.
func Link(st *store.Store, ref string) (string, error) {
	state, err := st.Load()
	if err != nil {
		return "", err
	}
	entry, err := state.Find(ref)
	if err != nil {
		return "", err
	}
	return ingest.Link(entry.Server)
}
