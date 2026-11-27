package main

import (
	"fmt"

	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/store"
)

func cmdOn(st *store.Store, _ []string) error {
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
	if err != nil {
		return err
	}
	fmt.Println(describe(status))
	return nil
}

func cmdStatus(_ *store.Store, _ []string) error {
	status, err := app.Status()
	if err != nil {
		return err
	}
	fmt.Println(describe(status))
	return nil
}

func describe(s helper.Status) string {
	if s.Version != helper.Version {
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
	case s.Blocked:
		return fmt.Sprintf("BLOCKED: the tunnel through %s is gone and the guard holds; turn it on again or off", s.Server)
	case !s.Running:
		return "off"
	case !s.Captured:
		return fmt.Sprintf("NOT WORKING: the core runs with %s, policy %s, but the tunnel device or the local port is missing", s.Server, s.Policy)
	}
	since := s.Since.Local().Format("15:04:05")
	switch s.Mode {
	case helper.ModeSystem:
		return fmt.Sprintf("on: %s, policy %s, since %s; system proxy on 127.0.0.1:%d", s.Server, s.Policy, since, s.Port)
	case helper.ModeProxy:
		return fmt.Sprintf("on: %s, policy %s, since %s; proxy on 127.0.0.1:%d, the system is not told", s.Server, s.Policy, since, s.Port)
	}
	if s.Port != 0 {
		guard += fmt.Sprintf("; proxy on 127.0.0.1:%d", s.Port)
	}
	return fmt.Sprintf("on: %s, policy %s, since %s; %s", s.Server, s.Policy, since, guard)
}
