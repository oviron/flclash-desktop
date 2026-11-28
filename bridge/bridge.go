// The bridge is the engine as a C library, for the native windows to link.
// The window runs on the shared model: FlcStart sets it going and hands
// every event to a function of the window's, FlcSend takes the window's
// messages, as internal/agent describes them. It links no core code: the
// core is a process of its own, which the app starts for the local port and
// the system proxy, and the helper for the tunnel.
package main

/*
#include <stdlib.h>

typedef void (*FlcEmit)(const char *event);

static void flc_emit(FlcEmit emit, const char *event) { emit(event); }
*/
import "C"

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/oviron/flclash-desktop/internal/agent"
	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/build"
	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/model"
	"github.com/oviron/flclash-desktop/internal/say"
	"github.com/oviron/flclash-desktop/internal/store"
)

var (
	once  sync.Once
	local func([]byte)
	mdl   *model.Model
)

// FlcStart starts the model with the language the window shows. emit is
// called from one thread at a time, not the main one, with an event as
// JSON that lives until it returns. Only the first call counts.
//
//export FlcStart
func FlcStart(lang *C.char, emit C.FlcEmit) {
	language := C.GoString(lang)
	once.Do(func() {
		say.SetLanguage(language)
		startLocal()
		m := model.New(language)
		mdl = m
		a := &agent.Agent{
			Model:         m,
			InstallHelper: installHelper,
			RemoveHelper:  removeHelper,
		}
		a.App = func() agent.App {
			info := agent.App{Lang: language, Version: build.Version}
			if a.Updater != nil {
				s := a.Updater.State()
				info.Update = &s
			}
			return info
		}
		startUpdater(a, language)
		local, _ = a.Local(func(event []byte) {
			text := C.CString(string(event))
			defer C.free(unsafe.Pointer(text))
			C.flc_emit(emit, text)
		})
		m.Start()
	})
}

// FlcSend hands the model a message of the window's. It does not wait.
//
//export FlcSend
func FlcSend(message *C.char) {
	if local != nil {
		local([]byte(C.GoString(message)))
	}
}

// FlcStop turns off what runs, as the app quits. It returns once the system
// proxy is put back.
//
//export FlcStop
func FlcStop() {
	if mdl != nil {
		mdl.Quit()
	}
}

// startLocal lets the app run the local port and the system proxy itself;
// the core ships next to the app's executable. Should it fail, every mode
// goes to the helper, as it did before.
func startLocal() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	st, err := store.Open()
	if err != nil {
		return
	}
	_ = helper.RunLocal(filepath.Join(filepath.Dir(self), app.CoreName), st.CoreDir())
}

// installHelper installs the helper through the system's administrator
// prompt and waits for it.
func installHelper() error {
	return asAdministrator("helper install -uid " + strconv.Itoa(os.Getuid()))
}

// removeHelper removes the helper and what it keeps, the same way.
func removeHelper() error { return asAdministrator("helper uninstall") }

// asAdministrator runs the command line with args through the system's
// administrator prompt and waits for it.
func asAdministrator(args string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	tool := filepath.Join(filepath.Dir(self), "flclashctl")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	command := quote(tool) + " " + args
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(command)
	cmd := exec.Command("/usr/bin/osascript", "-e", `do shell script "`+escaped+`" with administrator privileges`)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(stderr.String())
		// -128 is the person pressing Cancel.
		if strings.Contains(text, "-128") {
			return agent.ErrCancelled
		}
		if text == "" {
			return err
		}
		return errors.New(text)
	}
	return nil
}

func main() {}
