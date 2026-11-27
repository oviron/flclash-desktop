// Package helper is the privileged side: a small supervisor that runs the core
// with the rights to create the tunnel, and the protocol to talk to it.
//
// The supervisor does not trust its caller. It takes a server and rules,
// checks both and builds the config itself, so nothing the caller sends
// reaches the core as written.
package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/oviron/flclash-desktop/internal/ingest"
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

// Operations of Request.Op.
const (
	OpApply  = "apply"
	OpStop   = "stop"
	OpStatus = "status"
)

// Request is one call; a connection carries one request and one response.
type Request struct {
	Op string `json:"op"`
	// Revision orders apply calls: the supervisor refuses one older than what
	// it runs, so a late request cannot undo a newer choice.
	Revision int64         `json:"revision,omitempty"`
	Server   ingest.Server `json:"server,omitempty"`
	Policy   string        `json:"policy,omitempty"`
	Rules    []string      `json:"rules,omitempty"`
}

// Status is what the supervisor knows to be true right now.
type Status struct {
	// Running: the core process is alive with a config it accepted.
	Running bool `json:"running"`
	// Captured: the tunnel device is present on the system. Only this, not
	// Running, means traffic is being captured.
	Captured bool      `json:"captured"`
	Server   string    `json:"server,omitempty"`
	Policy   string    `json:"policy,omitempty"`
	Revision int64     `json:"revision,omitempty"`
	Since    time.Time `json:"since,omitempty"`
}

// Response answers a request. Status is set also when Error is.
type Response struct {
	Error  string `json:"error,omitempty"`
	Status Status `json:"status"`
}

// Call sends one request to the supervisor.
func Call(req Request, timeout time.Duration) (Status, error) {
	conn, err := net.DialTimeout("unix", SocketPath(), 3*time.Second)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Status{}, errors.New("the helper is not installed: sudo flclashctl helper install")
		}
		if errors.Is(err, os.ErrPermission) {
			return Status{}, errors.New("the helper was installed for another user")
		}
		return Status{}, fmt.Errorf("the helper does not answer: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Status{}, err
	}
	var resp Response
	if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&resp); err != nil {
		return Status{}, fmt.Errorf("the helper gave no answer: %w", err)
	}
	if resp.Error != "" {
		return resp.Status, errors.New(resp.Error)
	}
	return resp.Status, nil
}
