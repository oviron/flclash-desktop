package helper

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
)

// Guard is the firewall side of the tunnel: rules that outlive the core.
//
// The routes of the tunnel only say where traffic should go. A socket bound to
// a network interface ignores them, the tunnel carries no IPv6, and when the
// core dies the routes go with it. The guard closes these three doors.
type Guard interface {
	// Engage loads the rules. While they are in place, IPv6 leaves the host
	// only towards the local network. With full set, the same holds for IPv4:
	// what does not go into the tunnel device, come from the core, or stay in
	// the local network is refused. An empty tun means the device is gone.
	Engage(tun string, coreGID int, full bool) error
	// Release removes the rules.
	Release() error
	// Holding reports whether the full rules are in place right now.
	Holding() bool
}

// CoreGID is the group the core runs under while the guard is on. The group
// has no members and no name: it exists so that a firewall rule can tell the
// connections of the core from all others, root's included.
const CoreGID = 49251

// The kinds of guard a Status reports.
const (
	GuardIPv6 = "ipv6" // IPv6 is closed; IPv4 past the tunnel is not
	GuardFull = "full" // nothing leaves past the tunnel
)

// guardLabel marks the rule that makes a guard full; Holding looks for it.
const guardLabel = "flclash-guard"

// local is where traffic may go without the tunnel: the private ranges, link
// and multicast scopes. The system routes these past the tunnel anyway.
var (
	localV4 = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "224.0.0.0/4", "255.255.255.255/32"}
	localV6 = []string{"fe80::/10", "fc00::/7", "ff00::/8"}
)

// pfRules writes the rules for pf. connected are the networks the host is in
// right now, for those of them that are outside the private ranges.
func pfRules(tun string, coreGID int, full bool, connected []string) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	// "flags any": a connection opened a moment before the rules were loaded
	// must go on, not only a new one.
	line("pass quick on lo0 all flags any")
	if tun != "" && full {
		line("pass quick on %s all flags any", tun)
	}
	if coreGID != 0 {
		line("pass out quick proto { tcp, udp } all group %d flags any", coreGID)
	}
	if full {
		// Names are resolved inside the tunnel or by the core, never by the
		// resolver of the local network: a name is a leak too.
		line("block return out quick proto { tcp, udp } to any port { 53, 853 }")
		line("pass out quick inet from any to { %s }", strings.Join(append(append([]string{}, localV4...), connected...), ", "))
	}
	line("pass out quick inet6 from any to { %s }", strings.Join(localV6, ", "))
	if full {
		line("block return out all label %q", guardLabel)
	} else {
		line("block return out inet6 all")
	}
	return b.String()
}

// connectedNetworks lists the IPv4 networks of the interfaces that are up,
// leaving out the loopback and the ranges the tunnel uses. A virtual machine
// or a container network sits there, and it is as local as the Wi-Fi.
func connectedNetworks() []string {
	tunnelRange := netip.MustParsePrefix("198.18.0.0/16")
	seen := map[string]bool{}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			prefix, err := netip.ParsePrefix(ipnet.String())
			if err != nil || !prefix.Addr().Is4() || prefix.Bits() < 8 {
				continue
			}
			prefix = prefix.Masked()
			if prefix.Addr().IsPrivate() || prefix.Addr().IsLinkLocalUnicast() || tunnelRange.Overlaps(prefix) {
				continue
			}
			seen[prefix.String()] = true
		}
	}
	var out []string
	for network := range seen {
		out = append(out, network)
	}
	sort.Strings(out)
	return out
}

// tunName finds the tunnel device by its address.
func tunName() string {
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				if addr, ok := netip.AddrFromSlice(ipnet.IP); ok && tunPrefix.Contains(addr.Unmap()) {
					return iface.Name
				}
			}
		}
	}
	return ""
}
