package policy

import (
	"fmt"
	"net/netip"
	"strings"
)

// Lists is a policy the way most people think of one: what goes directly,
// what goes through the server, what is refused, and where the rest goes.
//
// It is a narrower thing than a policy: the rules of a list policy stand in
// one order, refusals first, then direct, then the server. A refusal thus
// always wins, and an exception to the rest is found before the rest is.
type Lists struct {
	Direct []string
	Proxy  []string
	Block  []string
	// Rest is where everything else goes: Proxy or Direct.
	Rest string
}

// An entry of a list is a line. A bare name stands for the name and all under
// it, a bare address or network for itself; anything else says its kind first.
var entryKinds = map[string]string{
	"domain":  "DOMAIN",
	"suffix":  "DOMAIN-SUFFIX",
	"keyword": "DOMAIN-KEYWORD",
	"geosite": "GEOSITE",
	"geoip":   "GEOIP",
}

// rule turns an entry into a rule that sends it to the destination.
func entryRule(entry, destination string) (string, error) {
	entry = strings.TrimSpace(entry)
	if kind, value, said := strings.Cut(entry, ":"); said && !strings.Contains(kind, ".") && netipFree(entry) {
		kind = strings.ToLower(strings.TrimSpace(kind))
		value = strings.TrimSpace(value)
		if kind == "ip" {
			return addressRule(value, destination)
		}
		ruleKind, known := entryKinds[kind]
		if !known {
			return "", fmt.Errorf("%q: unknown kind %q; use domain:, suffix:, keyword:, ip:, geosite: or geoip:", entry, kind)
		}
		return parseRule(ruleKind + "," + value + "," + destination)
	}
	if rule, err := addressRule(entry, destination); err == nil {
		return rule, nil
	}
	name := strings.TrimPrefix(strings.TrimPrefix(entry, "*"), ".")
	if name == "" || strings.ContainsAny(name, " ,/") {
		return "", fmt.Errorf("%q is neither a name like example.com nor a network like 10.0.0.0/8", entry)
	}
	return parseRule("DOMAIN-SUFFIX," + name + "," + destination)
}

// netipFree reports whether the entry is not an IPv6 address or network, whose
// colons are not a kind.
func netipFree(entry string) bool {
	_, err := prefixOf(entry)
	return err != nil
}

func prefixOf(text string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(text); err == nil {
		return prefix, nil
	}
	addr, err := netip.ParseAddr(text)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func addressRule(text, destination string) (string, error) {
	prefix, err := prefixOf(text)
	if err != nil {
		return "", fmt.Errorf("%q is not an address or a network like 10.0.0.0/8", text)
	}
	kind := "IP-CIDR"
	if !prefix.Addr().Is4() {
		kind = "IP-CIDR6"
	}
	return parseRule(kind + "," + prefix.String() + "," + destination)
}

// Text writes the lists as a policy document.
func (l Lists) Text() (string, error) {
	rest := strings.ToUpper(strings.TrimSpace(l.Rest))
	if rest != Proxy && rest != Direct {
		return "", fmt.Errorf("the rest goes through the server or directly, not to %q", l.Rest)
	}
	var b strings.Builder
	for _, part := range []struct {
		name, destination string
		entries           []string
	}{
		{"the list of what is refused", Reject, l.Block},
		{"the list of what goes directly", Direct, l.Direct},
		{"the list of what goes through the server", Proxy, l.Proxy},
	} {
		n := 0
		for _, entry := range part.entries {
			if cut := strings.IndexByte(entry, '#'); cut >= 0 {
				entry = entry[:cut]
			}
			if strings.TrimSpace(entry) == "" {
				continue
			}
			n++
			rule, err := entryRule(entry, part.destination)
			if err != nil {
				return "", fmt.Errorf("%s, entry %d: %w", part.name, n, err)
			}
			b.WriteString(rule + "\n")
		}
	}
	b.WriteString("MATCH," + rest + "\n")
	return b.String(), nil
}

// ListsOf reads rules back as lists. It says no for rules that are not in the
// order lists are written in: showing those as lists would change what they mean.
func ListsOf(rules []string) (Lists, bool) {
	var l Lists
	order := map[string]int{Reject: 0, Direct: 1, Proxy: 2}
	stage := 0
	for i, rule := range rules {
		parts := strings.Split(rule, ",")
		if parts[0] == "MATCH" {
			if i != len(rules)-1 || len(parts) != 2 || parts[1] == Reject {
				return Lists{}, false
			}
			l.Rest = parts[1]
			return l, true
		}
		if len(parts) < 3 {
			return Lists{}, false
		}
		at, known := order[parts[2]]
		if !known || at < stage {
			return Lists{}, false
		}
		stage = at
		var entry string
		switch parts[0] {
		case "DOMAIN-SUFFIX", "IP-CIDR", "IP-CIDR6":
			entry = parts[1]
		case "DOMAIN":
			entry = "domain:" + parts[1]
		case "DOMAIN-KEYWORD":
			entry = "keyword:" + parts[1]
		case "GEOSITE":
			entry = "geosite:" + parts[1]
		case "GEOIP":
			entry = "geoip:" + parts[1]
		default:
			return Lists{}, false
		}
		switch parts[2] {
		case Reject:
			l.Block = append(l.Block, entry)
		case Direct:
			l.Direct = append(l.Direct, entry)
		case Proxy:
			l.Proxy = append(l.Proxy, entry)
		}
	}
	return Lists{}, false
}

// BuiltinText returns the document of a built-in policy.
func BuiltinText(name string) (string, bool) {
	text, ok := builtin[name]
	return text, ok
}
