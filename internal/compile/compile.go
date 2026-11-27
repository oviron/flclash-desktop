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
	if in.LocalPort != 0 {
		cfg["mixed-port"] = in.LocalPort
		// With no TUN there is nothing to hijack; names resolve through the system.
		cfg["dns"] = map[string]any{"enable": false}
	}
	return yaml.Marshal(cfg)
}
