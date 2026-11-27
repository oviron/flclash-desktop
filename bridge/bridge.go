// The bridge is the engine as a C library, for the native windows to link.
// It has one entry point: a JSON request in, a JSON reply out. It links no
// core code; the tunnel is the helper's business.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	"github.com/oviron/flclash-desktop/internal/bridge"
)

// FlcCall answers one request. The caller frees the result with FlcFree.
//
//export FlcCall
func FlcCall(input *C.char) *C.char {
	return C.CString(string(bridge.Call([]byte(C.GoString(input)))))
}

// FlcFree releases a string FlcCall returned.
//
//export FlcFree
func FlcFree(p *C.char) { C.free(unsafe.Pointer(p)) }

func main() {}
