// The bridge is the engine as a C library, for the native windows to link.
// The window runs on the shared model: FlcStart sets it going and hands
// every event to a function of the window's, FlcSend takes the window's
// messages, as internal/agent describes them. It links no core code; the
// tunnel is the helper's business.
package main

/*
#include <stdlib.h>

typedef void (*FlcEmit)(const char *event);

static void flc_emit(FlcEmit emit, const char *event) { emit(event); }
*/
import "C"

import (
	"sync"
	"unsafe"

	"github.com/oviron/flclash-desktop/internal/agent"
	"github.com/oviron/flclash-desktop/internal/model"
)

var (
	once  sync.Once
	local func([]byte)
)

// FlcStart starts the model with the language the window shows. emit is
// called from one thread at a time, not the main one, with an event as
// JSON that lives until it returns. Only the first call counts.
//
//export FlcStart
func FlcStart(lang *C.char, emit C.FlcEmit) {
	language := C.GoString(lang)
	once.Do(func() {
		m := model.New(language)
		a := &agent.Agent{Model: m, App: func() agent.App { return agent.App{Lang: language} }}
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

func main() {}
