package helper

import (
	"net"
	"os"
	"path/filepath"
	"time"

	winio "github.com/tailscale/go-winio"

	"github.com/oviron/flclash-desktop/internal/say"
)

// PipeName is where the supervisor listens: a named pipe that only the
// system and the user the helper was installed for may open.
const PipeName = `\\.\pipe\flclash-helper`

// SocketPath is the pipe the supervisor listens on.
func SocketPath() string {
	if path := os.Getenv("FLCLASH_HELPER_SOCK"); path != "" {
		return path
	}
	return PipeName
}

// BaseDir holds what the helper keeps: the files of the cores it runs and its
// log. The system and administrators own it; the user it serves may read it.
func BaseDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "FlClash")
}

// StateDir is where the core that runs as the system keeps its files.
func StateDir() string { return filepath.Join(BaseDir(), "state") }

// LogPath is the file the helper and the cores it runs write to. It names
// servers and destinations, and holds no credentials.
func LogPath() string { return filepath.Join(BaseDir(), "helper.log") }

func dial(timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(SocketPath(), &timeout)
}

func notInstalled() error {
	return say.New("the helper is not installed: run flclashctl helper install as administrator")
}
