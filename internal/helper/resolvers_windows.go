package helper

import (
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

// systemResolvers reads the resolvers of the network adapters that are up,
// as they are while no tunnel is up. The tunnel's own address range is left
// out: it would be the core asking itself.
func systemResolvers() []string {
	size := uint32(15 << 10)
	var buf []byte
	for range 3 {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_SKIP_ANYCAST|windows.GAA_FLAG_SKIP_MULTICAST, 0,
			(*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil
		}
	}
	fakeRange := netip.MustParsePrefix("198.18.0.0/15")
	seen := map[string]bool{}
	var out []string
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		if a.OperStatus != windows.IfOperStatusUp || a.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK {
			continue
		}
		for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
			ip := d.Address.IP()
			addr, ok := netip.AddrFromSlice(ip)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			if fakeRange.Contains(addr) || addr.IsUnspecified() || addr.IsLinkLocalUnicast() || addr.IsLoopback() || seen[addr.String()] {
				continue
			}
			// Windows keeps fec0:0:0:ffff::1-3 as placeholders when it has no
			// IPv6 resolver; they answer nothing.
			if addr.Is6() && addr.As16()[0] == 0xfe && addr.As16()[1] == 0xc0 {
				continue
			}
			seen[addr.String()] = true
			out = append(out, addr.String())
		}
	}
	return out
}
