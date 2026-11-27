// Package compile builds the config the core runs from a server and a policy.
package compile

import (
	"errors"
	"fmt"
	"strings"

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
	// DirectDNS are the resolvers of the network the host is on, read before
	// the tunnel took the system resolver over. Names that go out directly are
	// resolved through them, as they would be with no tunnel.
	DirectDNS []string
}

// Proxy is a server the way the core takes one: its settings under its name.
func Proxy(server ingest.Server) map[string]any {
	proxy := map[string]any{"name": server.Name}
	for key, value := range server.Proxy {
		proxy[key] = value
	}
	return proxy
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
	proxy := Proxy(in.Server)
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
	}
	rules, providers := lists(in.Policy.Rules)
	cfg["rules"] = rules
	if len(providers) > 0 {
		cfg["rule-providers"] = providers
	}
	if in.LocalPort == 0 && !in.TUN {
		return nil, errors.New("the config captures nothing: neither TUN nor a local port")
	}
	if in.LocalPort != 0 {
		cfg["mixed-port"] = in.LocalPort
	}
	if in.TUN {
		tun(cfg, in.OS, in.DirectDNS)
	} else {
		// With no TUN there is nothing to hijack; names resolve through the system.
		cfg["dns"] = map[string]any{"enable": false}
	}
	return yaml.Marshal(cfg)
}

func tun(cfg map[string]any, os string, directDNS []string) {
	// Once the tunnel is up the system resolver is the core itself, so the
	// core cannot ask "the system" for the address of a direct destination or
	// of its own server. It asks the network's resolvers, with a public one
	// behind them for a network that changed since they were read.
	outside := append(append([]string{}, directDNS...), "https://9.9.9.9/dns-query")
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
		"respect-rules":           true,
		"nameserver":              []string{"https://9.9.9.9/dns-query"},
		"default-nameserver":      []string{"9.9.9.9", "94.140.14.14"},
		"proxy-server-nameserver": outside,
		"direct-nameserver":       outside,
	}
}

// catalogue is where site and country lists come from: the lists MetaCubeX
// builds daily from the community's domain and address data, one small binary
// file per list.
const catalogue = "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/"

// lists turns the GEOSITE and GEOIP rules of a policy into rule sets the core
// downloads one by one, a few kilobytes each, instead of the whole databases,
// which run to megabytes and would have to arrive before the first connection.
func lists(rules []string) ([]string, map[string]any) {
	out := make([]string, 0, len(rules))
	providers := map[string]any{}
	for _, rule := range rules {
		parts := strings.Split(rule, ",")
		if len(parts) != 3 || (parts[0] != "GEOSITE" && parts[0] != "GEOIP") {
			out = append(out, rule)
			continue
		}
		kind, behavior, suffix := "geosite", "domain", ""
		if parts[0] == "GEOIP" {
			// An address rule must not cost a DNS lookup of every name.
			kind, behavior, suffix = "geoip", "ipcidr", ",no-resolve"
		}
		name := kind + "-" + parts[1]
		providers[name] = map[string]any{
			"type":     "http",
			"behavior": behavior,
			"format":   "mrs",
			"url":      catalogue + kind + "/" + parts[1] + ".mrs",
			"path":     "./ruleset/" + name + ".mrs",
			"interval": 86400,
			// Fetched through the server: the catalogue's host is not reachable
			// from every network, and the tunnel is not up yet when this runs.
			"proxy": policy.Proxy,
		}
		out = append(out, "RULE-SET,"+name+","+parts[2]+suffix)
	}
	return out, providers
}
