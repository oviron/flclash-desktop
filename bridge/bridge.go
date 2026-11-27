// The bridge is the engine as a C library, for the native windows to link.
// It has one entry point: a JSON request in, a JSON reply out. It links no
// core code; the tunnel is the helper's business.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"strconv"
	"strings"
	"unsafe"

	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/store"
)

type request struct {
	Op     string `json:"op"`
	Group  string `json:"group"`
	Text   string `json:"text"`
	Server string `json:"server"`
	Policy string `json:"policy"`
	Name   string `json:"name"`
}

type status struct {
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
	Mode         string `json:"mode"`
	Port         int    `json:"port"`
	Guard        string `json:"guard"`
	GuardProblem string `json:"guardProblem"`
	// Blocked: the tunnel is gone and the guard holds the host closed.
	Blocked bool `json:"blocked"`
}

type reply struct {
	Error    string            `json:"error,omitempty"`
	View     *app.View         `json:"view,omitempty"`
	Status   *status           `json:"status,omitempty"`
	Imported *app.ImportResult `json:"imported,omitempty"`
	// Delay answers the delay operation, in milliseconds.
	Delay int `json:"delay,omitempty"`
	// Stats answers the stats operation.
	Stats *helper.Stats `json:"stats,omitempty"`
}

func toStatus(s helper.Status, err error) *status {
	out := &status{Installed: true, Running: s.Running, Captured: s.Captured, Server: s.Server, Policy: s.Policy,
		Mode: s.Mode, Port: s.Port, Guard: s.Guard, GuardProblem: s.GuardProblem, Blocked: s.Blocked}
	out.Outdated = err == nil && s.Version != helper.Version
	if !s.Since.IsZero() {
		out.Since = s.Since.Unix()
	}
	if err != nil && strings.Contains(err.Error(), "not installed") {
		out.Installed = false
	}
	return out
}

func handle(req request) (out reply) {
	fail := func(err error) reply {
		out.Error = err.Error()
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
	case "subRemove":
		if err := app.RemoveSubscription(st, req.Name); err != nil {
			return fail(err)
		}
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

// FlcCall answers one request. The caller frees the result with FlcFree.
//
//export FlcCall
func FlcCall(input *C.char) *C.char {
	var req request
	var out reply
	if err := json.Unmarshal([]byte(C.GoString(input)), &req); err != nil {
		out.Error = "bad request: " + err.Error()
	} else {
		out = handle(req)
	}
	raw, _ := json.Marshal(out)
	return C.CString(string(raw))
}

// FlcFree releases a string FlcCall returned.
//
//export FlcFree
func FlcFree(p *C.char) { C.free(unsafe.Pointer(p)) }

func main() {}
