//go:build !windows

package helper

import "os"

// systemResolvers reads the resolvers the system uses while no tunnel is up.
// The tunnel's own address range is left out: it would be the core asking itself.
func systemResolvers() []string {
	raw, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	return parseResolvers(string(raw))
}
