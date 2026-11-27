package helper

import (
	"net"
	"net/netip"

	"github.com/oviron/flclash-desktop/internal/wfp"
)

// NewWindowsGuard returns the guard of Windows: filters of the Windows
// Filtering Platform that the helper holds. They go away with the helper;
// a helper that starts again finds nothing held.
func NewWindowsGuard(corePath string) Guard {
	return &wfpGuard{core: corePath}
}

type wfpGuard struct {
	core  string
	rules wfp.Guard
}

func (g *wfpGuard) Engage(tun string, _ int, full bool) error {
	var luid uint64
	if tun != "" {
		iface, err := net.InterfaceByName(tun)
		if err != nil {
			return err
		}
		if luid, err = wfp.InterfaceLUID(iface.Index); err != nil {
			return err
		}
	}
	return g.rules.Apply(wfp.Rules{
		Core:    g.core,
		TunLUID: luid,
		Full:    full,
		LocalV4: prefixes(append(append([]string{}, localV4...), connectedNetworks()...)),
		LocalV6: prefixes(localV6),
	})
}

func (g *wfpGuard) Release() error {
	g.rules.Release()
	return nil
}

func (g *wfpGuard) Holding() bool { return g.rules.Holding() }

func prefixes(list []string) []netip.Prefix {
	var out []netip.Prefix
	for _, s := range list {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}
