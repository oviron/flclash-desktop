package helper

import (
	"reflect"
	"testing"
)

func TestReadingNetworksetup(t *testing.T) {
	services := parseServices("An asterisk (*) denotes that a network service is disabled.\nWi-Fi\n*Old Ethernet\nThunderbolt Bridge\n")
	if !reflect.DeepEqual(services, []string{"Wi-Fi", "Thunderbolt Bridge"}) {
		t.Fatalf("got %q", services)
	}
	proxy := parseProxy("Enabled: Yes\nServer: proxy.corp.example\nPort: 3128\nAuthenticated Proxy Enabled: 0\n")
	if proxy != (proxySetting{Enabled: true, Server: "proxy.corp.example", Port: 3128}) {
		t.Fatalf("got %+v", proxy)
	}
	if off := parseProxy("Enabled: No\nServer: \nPort: 0\n"); off.Enabled || off.Server != "" {
		t.Fatalf("got %+v", off)
	}
	if got := parseBypass("*.local\n169.254/16\n"); !reflect.DeepEqual(got, []string{"*.local", "169.254/16"}) {
		t.Fatalf("got %q", got)
	}
	if got := parseBypass("There aren't any bypass domains set on Wi-Fi.\n"); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}
