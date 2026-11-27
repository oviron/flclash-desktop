// Package policy reads a routing policy: an ordered list of rules that says
// where a captured connection goes.
package policy

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// The three places a rule can send a connection. Proxy stands for whichever
// server is selected.
const (
	Proxy  = "PROXY"
	Direct = "DIRECT"
	Reject = "REJECT"
)

// Policy is a parsed policy; Rules are in mihomo's syntax, in order.
type Policy struct {
	Name  string
	Rules []string
}

// Built-in policies, by name.
var builtin = map[string]string{
	"all": "MATCH,PROXY\n",
	// Private ranges are listed rather than taken from a GeoIP base, so the
	// policy works before any base is downloaded. It applies to connections
	// that reached the core: the system routes most LAN traffic past it.
	"lan-direct": `IP-CIDR,10.0.0.0/8,DIRECT
IP-CIDR,172.16.0.0/12,DIRECT
IP-CIDR,192.168.0.0/16,DIRECT
IP-CIDR,169.254.0.0/16,DIRECT
IP-CIDR,127.0.0.0/8,DIRECT
IP-CIDR6,fc00::/7,DIRECT
IP-CIDR6,fe80::/10,DIRECT
IP-CIDR6,::1/128,DIRECT
MATCH,PROXY
`,
	// Russian sites go out directly, everything else through the server: the
	// .ru and .рф zones and the lists of Russian services. No .su: blocked
	// sites live there too.
	"ru-direct": `IP-CIDR,10.0.0.0/8,DIRECT
IP-CIDR,172.16.0.0/12,DIRECT
IP-CIDR,192.168.0.0/16,DIRECT
IP-CIDR,169.254.0.0/16,DIRECT
IP-CIDR,127.0.0.0/8,DIRECT
IP-CIDR6,fc00::/7,DIRECT
IP-CIDR6,fe80::/10,DIRECT
IP-CIDR6,::1/128,DIRECT
DOMAIN-SUFFIX,ru,DIRECT
DOMAIN-SUFFIX,xn--p1ai,DIRECT
GEOSITE,category-bank-ru,DIRECT
GEOSITE,category-gov-ru,DIRECT
GEOSITE,category-medicine-ru,DIRECT
GEOSITE,category-ecommerce-ru,DIRECT
GEOSITE,category-retail-ru,DIRECT
GEOSITE,category-media-ru,DIRECT
GEOSITE,category-entertainment-ru,DIRECT
GEOSITE,category-travel-ru,DIRECT
GEOSITE,yandex,DIRECT
GEOSITE,mailru,DIRECT
GEOSITE,vk,DIRECT
GEOSITE,rutube,DIRECT
GEOSITE,category-ru,DIRECT
MATCH,PROXY
`,
}

// BuiltinNames lists the built-in policies in the order to show them.
func BuiltinNames() []string { return []string{"all", "lan-direct", "ru-direct"} }

// Builtin returns a built-in policy.
func Builtin(name string) (Policy, bool) {
	text, ok := builtin[name]
	if !ok {
		return Policy{}, false
	}
	p, err := Parse(name, text)
	if err != nil {
		panic(err) // a built-in that does not parse is a programming error
	}
	return p, true
}

// Parse reads a policy document: one rule per line, `#` starts a comment.
// An error names the line, so a policy edited by hand can be fixed from it.
func Parse(name, text string) (Policy, error) {
	p := Policy{Name: name}
	lines := strings.Split(text, "\n")
	last := 0
	for i, line := range lines {
		if cut := strings.IndexByte(line, '#'); cut >= 0 {
			line = line[:cut]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if last != 0 && strings.HasPrefix(p.Rules[len(p.Rules)-1], "MATCH,") {
			return Policy{}, fmt.Errorf("line %d: a rule after MATCH never applies", i+1)
		}
		rule, err := parseRule(line)
		if err != nil {
			return Policy{}, fmt.Errorf("line %d: %w", i+1, err)
		}
		p.Rules = append(p.Rules, rule)
		last = i + 1
	}
	if last == 0 {
		return Policy{}, fmt.Errorf("the policy is empty: it needs at least `MATCH,%s`", Proxy)
	}
	if !strings.HasPrefix(p.Rules[len(p.Rules)-1], "MATCH,") {
		return Policy{}, fmt.Errorf("line %d: the last rule must be MATCH, so that every connection has a destination", last)
	}
	return p, nil
}

// listName is what a list in the catalogue may be called. It goes into a URL
// and a file name on the privileged side, so nothing else is let through.
var listName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func parseRule(line string) (string, error) {
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	kind := strings.ToUpper(parts[0])
	target := func(s string) (string, error) {
		switch t := strings.ToUpper(s); t {
		case Proxy, Direct, Reject:
			return t, nil
		}
		return "", fmt.Errorf("unknown destination %q: use %s, %s or %s", s, Proxy, Direct, Reject)
	}
	switch kind {
	case "MATCH":
		if len(parts) != 2 {
			return "", fmt.Errorf("MATCH takes a destination only")
		}
		t, err := target(parts[1])
		if err != nil {
			return "", err
		}
		return "MATCH," + t, nil
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD":
		if len(parts) != 3 || parts[1] == "" {
			return "", fmt.Errorf("%s takes a name and a destination", kind)
		}
		t, err := target(parts[2])
		if err != nil {
			return "", err
		}
		return kind + "," + strings.ToLower(parts[1]) + "," + t, nil
	case "GEOSITE", "GEOIP":
		// A list from the public catalogue of site and country lists; see
		// compile for where they come from.
		if len(parts) != 3 || !listName.MatchString(strings.ToLower(parts[1])) {
			return "", fmt.Errorf("%s takes the name of a list, like category-ru, and a destination", kind)
		}
		t, err := target(parts[2])
		if err != nil {
			return "", err
		}
		return kind + "," + strings.ToLower(parts[1]) + "," + t, nil
	case "IP-CIDR", "IP-CIDR6":
		// A written no-resolve is accepted, since every address rule gets it anyway.
		if len(parts) == 4 && strings.EqualFold(parts[3], "no-resolve") {
			parts = parts[:3]
		}
		if len(parts) != 3 {
			return "", fmt.Errorf("%s takes a network and a destination", kind)
		}
		prefix, err := netip.ParsePrefix(parts[1])
		if err != nil {
			return "", fmt.Errorf("%q is not a network like 10.0.0.0/8", parts[1])
		}
		if prefix.Addr().Is4() != (kind == "IP-CIDR") {
			return "", fmt.Errorf("%s does not fit %s: use IP-CIDR for IPv4 and IP-CIDR6 for IPv6", parts[1], kind)
		}
		t, err := target(parts[2])
		if err != nil {
			return "", err
		}
		// no-resolve: a rule on an address must not cost a DNS lookup of every name.
		return kind + "," + prefix.Masked().String() + "," + t + ",no-resolve", nil
	}
	return "", fmt.Errorf("rule type %q is not supported: use DOMAIN, DOMAIN-SUFFIX, DOMAIN-KEYWORD, IP-CIDR, IP-CIDR6, GEOSITE, GEOIP or MATCH", parts[0])
}

// Check refuses rules that are not exactly what Parse produces, so that rules
// handed across a privilege boundary cannot carry anything else.
func Check(rules []string) error {
	parsed, err := Parse("", strings.Join(rules, "\n"))
	if err != nil {
		return err
	}
	if len(parsed.Rules) != len(rules) {
		return fmt.Errorf("the rules are not in canonical form")
	}
	for i := range rules {
		if parsed.Rules[i] != rules[i] {
			return fmt.Errorf("rule %d is not in canonical form", i+1)
		}
	}
	return nil
}
