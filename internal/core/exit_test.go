package core

import "testing"

func TestParseTrace(t *testing.T) {
	exit, ok := parseTrace("fl=1f2\nh=www.cloudflare.com\nip=203.0.113.7\nts=1\nloc=nl\ntls=TLSv1.3\n")
	if !ok || exit.IP != "203.0.113.7" || exit.Country != "NL" {
		t.Fatalf("got %+v, %v", exit, ok)
	}
	if exit, ok = parseTrace("ip=2001:db8::1\nloc=XX\n"); !ok || exit.IP != "2001:db8::1" || exit.Country != "" {
		t.Fatalf("got %+v, %v", exit, ok)
	}
	for _, body := range []string{"", "<html>blocked</html>", "ip=not-an-address\nloc=NL\n"} {
		if _, ok := parseTrace(body); ok {
			t.Errorf("%q has no address", body)
		}
	}
}
