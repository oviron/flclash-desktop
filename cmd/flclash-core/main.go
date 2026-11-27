// flclash-core is the core process the helper runs. It reads configs from
// stdin, each prefixed with its length as four big-endian bytes, applies them
// and answers every one with a JSON line on descriptor 3: {} or {"error": "..."}.
// Stdout stays free for the log of the core.
// When stdin closes, which also happens if the helper dies, it shuts the core
// down and exits, and the tunnel goes with it.
package main

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"os"

	"github.com/oviron/flclash-desktop/internal/core"
	"github.com/oviron/flclash-desktop/internal/helper"
)

func main() {
	out := json.NewEncoder(os.NewFile(3, "verdicts"))
	for {
		var size [4]byte
		if _, err := io.ReadFull(os.Stdin, size[:]); err != nil {
			break
		}
		config := make([]byte, binary.BigEndian.Uint32(size[:]))
		if _, err := io.ReadFull(os.Stdin, config); err != nil {
			break
		}
		reply := map[string]string{}
		if err := core.Apply(helper.StateDir(), config); err != nil {
			reply["error"] = err.Error()
		}
		if err := out.Encode(reply); err != nil {
			break
		}
	}
	core.Stop()
}
