package helper

// SystemProxy names a local port as the proxy of the system and puts back
// what was there before.
type SystemProxy interface {
	// Set points every network service at the loopback port.
	Set(port int) error
	// Clear restores what Set replaced. With nothing to restore it does nothing.
	Clear() error
}

// proxyBypass is what must not go to the proxy whatever the policy says: the
// host itself and the local network.
var proxyBypass = []string{"localhost", "127.0.0.1", "::1", "*.local", "169.254.0.0/16", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
