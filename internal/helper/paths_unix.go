//go:build !windows

package helper

import (
	"net"
	"os"
	"runtime"
	"time"

	"github.com/oviron/flclash-desktop/internal/say"
)

// SocketPath is where the supervisor listens. The socket file belongs to the
// user the helper was installed for, with access for that user alone.
func SocketPath() string {
	if path := os.Getenv("FLCLASH_HELPER_SOCK"); path != "" {
		return path
	}
	return "/var/run/flclash-helper.sock"
}

// StateDir is where the root core keeps its files.
func StateDir() string {
	if runtime.GOOS == "darwin" {
		return "/var/db/flclash-desktop"
	}
	return "/var/lib/flclash-desktop"
}

// LogPath is the file the helper and the cores it runs write to. Anyone may
// read it: it names servers and destinations, and holds no credentials.
func LogPath() string { return "/var/log/flclash-helper.log" }

func dial(timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", SocketPath(), timeout)
}

func notInstalled() error {
	return say.New("the helper is not installed: sudo flclashctl helper install")
}
