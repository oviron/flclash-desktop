// Package bridge is the engine as the windows see it: one request in, one
// reply out. The shared model sends them; the windows of both systems reach
// the model through internal/agent.
package bridge

import (
	"os"
	"strconv"
	"strings"

	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/policy"
	"github.com/oviron/flclash-desktop/internal/say"
	"github.com/oviron/flclash-desktop/internal/store"
)

// Request is one call from a window.
type Request struct {
	Op     string `json:"op"`
	Group  string `json:"group"`
	Text   string `json:"text"`
	Server string `json:"server"`
	Policy string `json:"policy"`
	Name   string `json:"name"`
	// The lists of a policy, a line per entry, and where the rest goes.
	Direct string `json:"direct"`
	Proxy  string `json:"proxy"`
	Block  string `json:"block"`
	Rest   string `json:"rest"`
	// The settings of a subscription.
	Agent string `json:"agent"`
	Every string `json:"every"`
	Skip  string `json:"skip"`
	// Lang is the language the app shows; messages are written in it.
	Lang string `json:"lang"`
}

// Status is what a window shows of the helper.
type Status struct {
	// Installed is false when there is no helper to ask.
	Installed bool `json:"installed"`
	// Outdated: the installed helper is from another version of the app and
	// has to be installed again before it will take this app's requests.
	Outdated bool   `json:"outdated"`
	Running  bool   `json:"running"`
	Captured bool   `json:"captured"`
	Server   string `json:"server"`
	Policy   string `json:"policy"`
	Since    int64  `json:"since"` // unix seconds, 0 when off
	// Guard is "full" when nothing leaves past the tunnel, "ipv6" when only
	// IPv6 is closed, empty when there is no guard.
	// Mode and Port say what runs: "tun", "system" or "proxy", and the local port.
	Mode string `json:"mode"`
	Port int    `json:"port"`
	// LAN: the running port is open to the local network too.
	LAN          bool   `json:"lan"`
	Guard        string `json:"guard"`
	GuardProblem string `json:"guardProblem"`
	// Blocked: the tunnel is gone and the guard holds the host closed.
	Blocked bool `json:"blocked"`
}

// Reply answers a Request.
type Reply struct {
	Error    string            `json:"error,omitempty"`
	View     *app.View         `json:"view,omitempty"`
	Status   *Status           `json:"status,omitempty"`
	Imported *app.ImportResult `json:"imported,omitempty"`
	// Delay answers the delay operation, in milliseconds.
	Delay int `json:"delay,omitempty"`
	// Stats answers the stats operation.
	Stats *helper.Stats `json:"stats,omitempty"`
	// Link answers the link operation: a server as a share link.
	Link string `json:"link,omitempty"`
	// Exit answers the exit operation: where the running server lets traffic out.
	Exit *helper.Exit `json:"exit,omitempty"`
	// Log answers the log operation: the end of the helper's log.
	Log string `json:"log,omitempty"`
	// Document answers policyRead.
	Document *app.PolicyDoc `json:"document,omitempty"`
}

func toStatus(s helper.Status, err error) *Status {
	out := &Status{Installed: true, Running: s.Running, Captured: s.Captured, Server: s.Server, Policy: s.Policy,
		Mode: s.Mode, Port: s.Port, LAN: s.LAN, Guard: s.Guard, GuardProblem: s.GuardProblem, Blocked: s.Blocked}
	out.Outdated = err == nil && s.Version != helper.Version
	if !s.Since.IsZero() {
		out.Since = s.Since.Unix()
	}
	if err != nil && strings.Contains(err.Error(), "not installed") {
		out.Installed = false
	}
	return out
}

// Handle answers one request. Calls may come from several goroutines.
func Handle(req Request) (out Reply) {
	say.SetLanguage(req.Lang)
	fail := func(err error) Reply {
		out.Error = say.Text(err)
		return out
	}
	st, err := store.Open()
	if err != nil {
		return fail(err)
	}
	switch req.Op {
	case "snapshot":
	case "status":
		s, err := app.Status()
		out.Status = toStatus(s, err)
		// A missing helper is a state to show, not a failure of the call.
		if err != nil && out.Status.Installed {
			return fail(err)
		}
		return out
	case "delay":
		ms, err := app.Delay(st, req.Server)
		if err != nil {
			return fail(err)
		}
		out.Delay = ms
		return out
	case "link":
		link, err := app.Link(st, req.Server)
		if err != nil {
			return fail(err)
		}
		out.Link = link
		return out
	case "exit":
		exit, err := helper.ReadExit()
		if err != nil {
			return fail(err)
		}
		// The list shows the country of the server too.
		if state, err := st.Load(); err == nil && exit.Country != "" {
			app.RememberCountry(st, state.Selected, exit.Country, true)
		}
		out.Exit = &exit
		return out
	case "log":
		out.Log = logTail(256 << 10)
		return out
	case "stats":
		stats, err := helper.ReadStats()
		if err != nil {
			return fail(err)
		}
		out.Stats = &stats
		return out
	case "on":
		s, err := app.On(st)
		out.Status = toStatus(s, err)
		if err != nil {
			return fail(err)
		}
	case "off":
		s, err := app.Off()
		out.Status = toStatus(s, err)
		if err != nil {
			return fail(err)
		}
	case "import":
		group := req.Group
		if group == "" {
			group = "default"
		}
		result, err := app.Import(st, group, req.Text)
		out.Imported = &result
		if err != nil {
			return fail(err)
		}
	case "select":
		if err := app.Select(st, req.Server, req.Policy); err != nil {
			return fail(err)
		}
	case "remove":
		if err := app.Remove(st, req.Server); err != nil {
			return fail(err)
		}
	case "policyImport":
		if err := st.ImportPolicy(req.Name, req.Text); err != nil {
			return fail(err)
		}
	case "policyRead":
		doc, err := app.ReadPolicy(st, req.Name)
		if err != nil {
			return fail(err)
		}
		out.Document = &doc
		return out
	case "policyWrite":
		if err := app.WritePolicy(st, req.Name, req.Text); err != nil {
			return fail(err)
		}
	case "policyWriteLists":
		lines := func(text string) []string { return strings.Split(text, "\n") }
		lists := policy.Lists{Direct: lines(req.Direct), Proxy: lines(req.Proxy), Block: lines(req.Block), Rest: req.Rest}
		if err := app.WritePolicyLists(st, req.Name, lists); err != nil {
			return fail(err)
		}
	case "mode":
		if err := app.SetMode(st, req.Text); err != nil {
			return fail(err)
		}
	case "port":
		port, _ := strconv.Atoi(req.Text)
		if err := app.SetPort(st, port); err != nil {
			return fail(err)
		}
	case "subAdd":
		result, err := app.AddSubscription(st, req.Name, req.Text)
		out.Imported = &result
		if err != nil {
			return fail(err)
		}
	case "subUpdate":
		if req.Name == "" {
			if _, err := app.UpdateDue(st); err != nil {
				return fail(err)
			}
			break
		}
		result, err := app.UpdateSubscription(st, req.Name)
		out.Imported = &result
		if err != nil {
			// The servers that were there stay; the list shows why it failed.
			if view, viewErr := app.Snapshot(st); viewErr == nil {
				out.View = &view
			}
			return fail(err)
		}
	case "subEdit":
		every, _ := strconv.Atoi(req.Every)
		result, err := app.EditSubscription(st, req.Name, req.Text, req.Agent, every, req.Skip)
		out.Imported = &result
		if err != nil {
			if view, viewErr := app.Snapshot(st); viewErr == nil {
				out.View = &view
			}
			return fail(err)
		}
	case "backupWrite":
		if err := app.WriteBackup(st, req.Name); err != nil {
			return fail(err)
		}
		return out
	case "backupRead":
		if err := app.ReadBackup(st, req.Name); err != nil {
			return fail(err)
		}
	case "subRemove":
		if err := app.RemoveSubscription(st, req.Name); err != nil {
			return fail(err)
		}
	case "lan":
		if err := app.SetLAN(st, req.Text == "on"); err != nil {
			return fail(err)
		}
	case "auth":
		if err := app.SetAuth(st, req.Text); err != nil {
			return fail(err)
		}
	case "dns":
		if err := app.SetDNS(st, req.Text); err != nil {
			return fail(err)
		}
	case "close":
		if err := helper.CloseConnection(req.Name); err != nil {
			return fail(err)
		}
		return out
	case "autoConnect":
		if err := app.SetAutoConnect(st, req.Text == "on"); err != nil {
			return fail(err)
		}
	case "protect":
		if err := app.Protect(st, req.Text == "on"); err != nil {
			return fail(err)
		}
	case "policyRemove":
		if err := app.RemovePolicy(st, req.Name); err != nil {
			return fail(err)
		}
	default:
		out.Error = "unknown operation " + req.Op
		return out
	}
	view, err := app.Snapshot(st)
	if err != nil {
		return fail(err)
	}
	out.View = &view
	return out
}

// logTail reads the end of the helper's log, from the start of a line.
func logTail(limit int64) string {
	file, err := os.Open(helper.LogPath())
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	start := max(info.Size()-limit, 0)
	raw := make([]byte, info.Size()-start)
	n, _ := file.ReadAt(raw, start)
	text := string(raw[:n])
	if start > 0 {
		if cut := strings.IndexByte(text, '\n'); cut >= 0 {
			text = text[cut+1:]
		}
	}
	return text
}
