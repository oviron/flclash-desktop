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

// Version says which requests the installed helper understands. Raise it
// whenever the helper or the core it runs has to be reinstalled for the app to
// work: a new rule type, a new server field, a change of this protocol.
const Version = 8

// Operations of Request.Op.
const (
	OpApply  = "apply"
	OpStop   = "stop"
	OpStatus = "status"
	// OpDelay measures a server from inside the running core; with the tunnel
	// off the caller runs the core itself, without privileges.
	OpDelay = "delay"
	// OpStats reads the traffic counters and the open connections of the core.
	OpStats = "stats"
)

// The measurement of a delay is one request for DelayURL through the server,
// given DelayTimeout to answer. The helper sets both: its caller does not get
// to send the root core wherever it likes.
const (
	DelayURL     = "https://www.gstatic.com/generate_204"
	DelayTimeout = 5 * time.Second
)

// Frames the helper writes to the core: a length, a kind, a body.
const (
	FrameConfig = 'c' // body: the config
	FrameDelay  = 'd' // body: a CoreDelay
	FrameStats  = 's' // no body; the answer carries Stats
)

// Stats is the traffic of the core at one moment.
type Stats struct {
	// Up and Down are bytes per second right now; the totals count from the
	// start of the core.
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
	UpTotal   int64 `json:"upTotal"`
	DownTotal int64 `json:"downTotal"`
	// Connections are the open ones, newest first, at most MaxConnections.
	Connections []Connection `json:"connections"`
	// Open counts all open connections, also those left out of the list.
	Open int `json:"open"`
}

// MaxConnections bounds the list in Stats: an answer has to stay small.
const MaxConnections = 300

// Connection is one open connection as the core sees it.
type Connection struct {
	ID string `json:"id"`
	// Host is the name it goes to, or the address when the name is not known.
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Network string `json:"network"` // tcp or udp
	// Via is where the policy sent it: the server, DIRECT or REJECT.
	Via string `json:"via"`
	// Rule is the rule that decided, as written in the policy.
	Rule     string `json:"rule"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Start    int64  `json:"start"` // unix seconds
}

// CoreDelay asks the core to measure one server.
type CoreDelay struct {
	Proxy     map[string]any `json:"proxy"`
	URL       string         `json:"url"`
	TimeoutMS int            `json:"timeout_ms"`
}

// CoreReply is the core's answer to a frame, one JSON line.
type CoreReply struct {
	Error string `json:"error,omitempty"`
	Delay int    `json:"delay,omitempty"`
	Stats *Stats `json:"stats,omitempty"`
}

// The ways the core can serve the host, Request.Mode. They differ in what the
// system is told, and in the rights the core runs with.
const (
	// ModeTUN captures the traffic of the whole host through a tunnel device.
	// The core runs as root. An empty Mode means this one.
	ModeTUN = "tun"
	// ModeSystem opens the local port and names it as the proxy of the system:
	// programs that follow the system setting go through it.
	ModeSystem = "system"
	// ModeProxy opens the local port and changes nothing else: only programs
	// pointed at the port go through it.
	ModeProxy = "proxy"
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
	// Mode is how the host is served: ModeTUN, ModeSystem or ModeProxy.
	Mode string `json:"mode,omitempty"`
	// Port is the loopback port for HTTP and SOCKS. The proxy modes need one;
	// with the tunnel it is an extra door, and zero leaves it shut.
	Port int `json:"port,omitempty"`
	// Protect asks that nothing go past the core, also when the core dies.
	// With the tunnel that is the full guard; without Protect only IPv6 is
	// closed while the tunnel is up. In the system mode it means the system
	// proxy stays set when the core dies, so that programs stop rather than
	// go out directly.
	Protect bool `json:"protect,omitempty"`
}

// Status is what the supervisor knows to be true right now.
type Status struct {
	// Running: the core process is alive with a config it accepted.
	Running bool `json:"running"`
	// Captured: what the mode promises is in place: the tunnel device is
	// present, or the local port answers. Only this, not Running, means
	// traffic can go through.
	Captured bool      `json:"captured"`
	Mode     string    `json:"mode,omitempty"`
	Port     int       `json:"port,omitempty"`
	Server   string    `json:"server,omitempty"`
	Policy   string    `json:"policy,omitempty"`
	Revision int64     `json:"revision,omitempty"`
	Since    time.Time `json:"since,omitempty"`
	// Guard is the guard that is on: GuardIPv6, GuardFull or none.
	Guard string `json:"guard,omitempty"`
	// GuardProblem says why the guard that was asked for is not on.
	GuardProblem string `json:"guard_problem,omitempty"`
	// Blocked: the core is gone and what Protect asked for holds: the full
	// guard, so that nothing leaves the host but local traffic, or the system
	// proxy, so that programs which follow it stay offline. It lasts until
	// the core is back or it is turned off.
	Blocked bool `json:"blocked,omitempty"`
	// Version is the Version of the helper that answers; a helper from before
	// versions existed leaves it zero.
	Version int `json:"version,omitempty"`
}

// Response answers a request. Status is set also when Error is.
type Response struct {
	Error  string `json:"error,omitempty"`
	Status Status `json:"status"`
	// Delay answers OpDelay, in milliseconds.
	Delay int `json:"delay,omitempty"`
	// Stats answers OpStats.
	Stats *Stats `json:"stats,omitempty"`
}

// Call sends one request to the supervisor.
func Call(req Request, timeout time.Duration) (Status, error) {
	resp, err := call(req, timeout)
	return resp.Status, err
}

// ReadStats reads the traffic of the running core.
func ReadStats() (Stats, error) {
	resp, err := call(Request{Op: OpStats}, 10*time.Second)
	if err != nil || resp.Stats == nil {
		return Stats{}, err
	}
	return *resp.Stats, nil
}

// Delay measures a server through the running core.
func Delay(server ingest.Server) (int, error) {
	resp, err := call(Request{Op: OpDelay, Server: server}, DelayTimeout+10*time.Second)
	return resp.Delay, err
}

func call(req Request, timeout time.Duration) (Response, error) {
	var resp Response
	conn, err := net.DialTimeout("unix", SocketPath(), 3*time.Second)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return resp, errors.New("the helper is not installed: sudo flclashctl helper install")
		}
		if errors.Is(err, os.ErrPermission) {
			return resp, errors.New("the helper was installed for another user")
		}
		return resp, fmt.Errorf("the helper does not answer: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return resp, err
	}
	if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&resp); err != nil {
		return resp, fmt.Errorf("the helper gave no answer: %w", err)
	}
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}
