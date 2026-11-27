package main

import (
	"errors"
	"fmt"

	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/store"
)

// The command line reaches the helper only, and the helper runs the tunnel
// only: the local port and the system proxy run inside the app.
const inApp = "the local port and the system proxy run inside the FlClash app: turn them on there, " +
	"or use flclashctl run -local-port N for a core that runs while the command does"

func cmdOn(st *store.Store, _ []string) error {
	if state, err := st.Load(); err == nil && state.Mode != helper.ModeTUN {
		return errors.New(inApp)
	}
	status, err := app.On(st)
	if err != nil {
		if status.Running {
			return fmt.Errorf("%w; still running: %s", err, describe(status))
		}
		return err
	}
	fmt.Println(describe(status))
	return nil
}

func cmdOff(_ *store.Store, _ []string) error {
	status, err := app.Off()
	if helper.IsNotInstalled(err) {
		fmt.Println("the helper is not installed, so no tunnel runs; " + inApp)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Println(describe(status))
	return nil
}

func cmdStatus(_ *store.Store, _ []string) error {
	status, err := app.Status()
	if helper.IsNotInstalled(err) {
		fmt.Println("no tunnel: the helper, which only the tunnel needs, is not installed; " + inApp)
		return nil
	}
	if err != nil {
		return err
	}
	text := describe(status)
	if !status.Running && !status.Blocked {
		text += " (the tunnel; the local port and the system proxy are shown in the app)"
	}
	fmt.Println(text)
	return nil
}

func describe(s helper.Status) string {
	if !s.Current() {
		return "the installed helper is from another version of the app: sudo flclashctl helper install"
	}
	guard := map[string]string{
		"":               "no guard: traffic can leave past the tunnel",
		helper.GuardIPv6: "IPv6 closed",
		helper.GuardFull: "nothing leaves past the tunnel",
	}[s.Guard]
	if s.GuardProblem != "" {
		guard += " (" + s.GuardProblem + ")"
	}
	switch {
	case s.Blocked && s.Mode == helper.ModeSystem:
		return fmt.Sprintf("BLOCKED: the core that ran through %s is gone and the system proxy stays set; turn it on again or off", s.Server)
	case s.Blocked:
		return fmt.Sprintf("BLOCKED: the tunnel through %s is gone and the guard holds; turn it on again or off", s.Server)
	case !s.Running:
		return "off"
	case !s.Captured:
		return fmt.Sprintf("NOT WORKING: the core runs with %s, policy %s, but the tunnel device or the local port is missing", s.Server, s.Policy)
	}
	since := s.Since.Local().Format("15:04:05")
	// Open to the network, the port is reached by this host's addresses there too.
	lan := ""
	if s.LAN {
		lan = "; the port is open to the local network too"
	}
	switch s.Mode {
	case helper.ModeSystem:
		return fmt.Sprintf("on: %s, policy %s, since %s; system proxy on 127.0.0.1:%d%s", s.Server, s.Policy, since, s.Port, lan)
	case helper.ModeProxy:
		return fmt.Sprintf("on: %s, policy %s, since %s; proxy on 127.0.0.1:%d, the system is not told%s", s.Server, s.Policy, since, s.Port, lan)
	}
	if s.Port != 0 {
		guard += fmt.Sprintf("; proxy on 127.0.0.1:%d%s", s.Port, lan)
	}
	return fmt.Sprintf("on: %s, policy %s, since %s; %s", s.Server, s.Policy, since, guard)
}
