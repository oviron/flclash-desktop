package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/store"
)

func cmdOn(st *store.Store, _ []string) error {
	state, err := st.Load()
	if err != nil {
		return err
	}
	if state.Selected == "" {
		return errors.New("no server is selected: flclashctl select SERVER")
	}
	entry, err := state.Find(state.Selected)
	if err != nil {
		return err
	}
	pol, err := st.Policy(state.Policy)
	if err != nil {
		return err
	}
	status, err := helper.Call(helper.Request{
		Op:       helper.OpApply,
		Revision: time.Now().UnixNano(),
		Server:   entry.Server,
		Policy:   pol.Name,
		Rules:    pol.Rules,
	}, 90*time.Second)
	if err != nil {
		// What is still true matters most when the change did not go through.
		if status.Running {
			return fmt.Errorf("%w; still running: %s", err, describe(status))
		}
		return err
	}
	fmt.Println(describe(status))
	return nil
}

func cmdOff(_ *store.Store, _ []string) error {
	status, err := helper.Call(helper.Request{Op: helper.OpStop}, 30*time.Second)
	if err != nil {
		return err
	}
	fmt.Println(describe(status))
	return nil
}

func cmdStatus(_ *store.Store, _ []string) error {
	status, err := helper.Call(helper.Request{Op: helper.OpStatus}, 10*time.Second)
	if err != nil {
		return err
	}
	fmt.Println(describe(status))
	return nil
}

func describe(s helper.Status) string {
	switch {
	case !s.Running:
		return "off"
	case !s.Captured:
		return fmt.Sprintf("NOT CAPTURING: the core runs with %s, policy %s, but the tunnel device is missing", s.Server, s.Policy)
	}
	return fmt.Sprintf("on: %s, policy %s, since %s", s.Server, s.Policy, s.Since.Local().Format("15:04:05"))
}
