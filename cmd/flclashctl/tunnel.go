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
	switch {
	case !s.Running:
		return "off"
	case !s.Captured:
		return fmt.Sprintf("NOT CAPTURING: the core runs with %s, policy %s, but the tunnel device is missing", s.Server, s.Policy)
	}
	return fmt.Sprintf("on: %s, policy %s, since %s", s.Server, s.Policy, s.Since.Local().Format("15:04:05"))
}
