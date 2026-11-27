// Package compile builds the config the core runs from a server and a policy.
package compile

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/oviron/flclash-desktop/internal/ingest"
	"github.com/oviron/flclash-desktop/internal/policy"
)

// Input is everything one running config is made of.
type Input struct {
	Server ingest.Server
	Policy policy.Policy
	// LocalPort, when not zero, opens a mixed HTTP/SOCKS listener on loopback.
	// It is how the core is checked without privileges; the product itself
	// captures traffic through TUN and opens no local port.
	LocalPort int
	// TUN captures the traffic of the whole host. The process needs the
	// privileges to create the device and change routes.
	TUN bool
	// OS is the system the config will run on, as in runtime.GOOS.
	OS string
}

// Config returns the YAML for the core.
func Config(in Input) ([]byte, error) {
	if in.Server.Name == "" || in.Server.Proxy == nil {
		return nil, errors.New("no server is selected")
	}
	if len(in.Policy.Rules) == 0 {
		return nil, errors.New("no routing policy is selected")
	}
	// The names a policy may send a connection to are taken; a server called
	// like one of them would change what the rules mean.
	switch in.Server.Name {
	case policy.Proxy, policy.Direct, policy.Reject:
		return nil, fmt.Errorf("a server cannot be named %q", in.Server.Name)
	}
	proxy := map[string]any{"name": in.Server.Name}
	for key, value := range in.Server.Proxy {
		proxy[key] = value
	}
	cfg := map[string]any{
		"mode":            "rule",
		"log-level":       "info",
		"allow-lan":       false,
		"bind-address":    "127.0.0.1",
		"geo-auto-update": false,
		"profile":         map[string]any{"store-selected": false, "store-fake-ip": false},
		"proxies":         []any{proxy},
		// The policy says PROXY; this group is what PROXY is.
		"proxy-groups": []any{map[string]any{
			"name":    policy.Proxy,
			"type":    "select",
			"proxies": []string{in.Server.Name},
		}},
		"rules": in.Policy.Rules,
	}
	if in.LocalPort == 0 && !in.TUN {
		return nil, errors.New("the config captures nothing: neither TUN nor a local port")
	}
	if in.LocalPort != 0 {
		cfg["mixed-port"] = in.LocalPort
	}
	if in.TUN {
		tun(cfg, in.OS)
	} else {
		// With no TUN there is nothing to hijack; names resolve through the system.
		cfg["dns"] = map[string]any{"enable": false}
	}
	return yaml.Marshal(cfg)
}

func tun(cfg map[string]any, os string) {
	device := map[string]any{
		"enable":                true,
		"stack":                 "mips",
		"auto-route":            true,
		"auto-detect-interface": true,
		"dns-hijack":            []string{"any:53", "tcp://any:53"},
	}
	if os == "darwin" {
		// On macOS the core leaves the system resolver alone unless asked, and
		// a query to a resolver on the LAN then never enters the device.
		// `native` registers a temporary resolver that goes away with the process.
		device["dns-mode"] = "native"
	}
	cfg["tun"] = device
	// IPv6 stays off in the core until it is also blocked outside the tunnel.
	cfg["ipv6"] = false
	// No rule looks at the process, so the core need not look it up.
	cfg["find-process-mode"] = "off"
	cfg["dns"] = map[string]any{
		"enable":        true,
		"ipv6":          false,
		"enhanced-mode": "fake-ip",
		"fake-ip-range": "198.18.0.1/16",
		// Queries follow the routing policy like any other connection.
		"respect-rules": true,
		"nameserver":    []string{"https://9.9.9.9/dns-query"},
		// Bootstrap for the names above and for the server itself: the system
		// resolver first, reached outside the tunnel.
		"default-nameserver":      []string{"9.9.9.9", "94.140.14.14"},
		"proxy-server-nameserver": []string{"system://", "https://9.9.9.9/dns-query"},
		"direct-nameserver":       []string{"system://"},
	}
}
