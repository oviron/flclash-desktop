package policy

import (
	"reflect"
	"strings"
	"testing"
)

func TestListsBecomeRulesAndComeBack(t *testing.T) {
	lists := Lists{
		Block:  []string{"ads.example", "keyword:tracker  # a note"},
		Direct: []string{".ru", "*.xn--p1ai", "10.0.0.0/8", "192.168.1.7", "fc00::/7", "geosite:yandex", "", "domain:exact.example"},
		Proxy:  []string{"geoip:us", "ip:1.1.1.1"},
		Rest:   "direct",
	}
	text, err := lists.Text()
	if err != nil {
		t.Fatal(err)
	}
	want := `DOMAIN-SUFFIX,ads.example,REJECT
DOMAIN-KEYWORD,tracker,REJECT
DOMAIN-SUFFIX,ru,DIRECT
DOMAIN-SUFFIX,xn--p1ai,DIRECT
IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
IP-CIDR,192.168.1.7/32,DIRECT,no-resolve
IP-CIDR6,fc00::/7,DIRECT,no-resolve
GEOSITE,yandex,DIRECT
DOMAIN,exact.example,DIRECT
GEOIP,us,PROXY
IP-CIDR,1.1.1.1/32,PROXY,no-resolve
MATCH,DIRECT
`
	if text != want {
		t.Fatalf("got:\n%s", text)
	}
	parsed, err := Parse("p", text)
	if err != nil {
		t.Fatal(err)
	}
	back, ok := ListsOf(parsed.Rules)
	if !ok || back.Rest != Direct ||
		!reflect.DeepEqual(back.Block, []string{"ads.example", "keyword:tracker"}) ||
		!reflect.DeepEqual(back.Direct, []string{"ru", "xn--p1ai", "10.0.0.0/8", "192.168.1.7/32", "fc00::/7", "geosite:yandex", "domain:exact.example"}) ||
		!reflect.DeepEqual(back.Proxy, []string{"geoip:us", "1.1.1.1/32"}) {
		t.Fatalf("got %+v, %v", back, ok)
	}
	// What comes back writes the same rules again.
	again, err := back.Text()
	if err != nil || again != text {
		t.Fatalf("got %v:\n%s", err, again)
	}
}

func TestBuiltinsReadAsLists(t *testing.T) {
	for _, name := range BuiltinNames() {
		p, _ := Builtin(name)
		if _, ok := ListsOf(p.Rules); !ok {
			t.Errorf("%s must read as lists, so that a copy of it can be edited as lists", name)
		}
	}
}

func TestRulesThatAreNotLists(t *testing.T) {
	for _, text := range []string{
		"DOMAIN-SUFFIX,a.example,PROXY\nDOMAIN-SUFFIX,b.example,DIRECT\nMATCH,PROXY\n",  // the server before direct
		"DOMAIN-SUFFIX,a.example,DIRECT\nDOMAIN-SUFFIX,b.example,REJECT\nMATCH,PROXY\n", // a refusal after direct
		"MATCH,REJECT\n",
	} {
		p, err := Parse("p", text)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ListsOf(p.Rules); ok {
			t.Errorf("these rules are not in the order of lists:\n%s", text)
		}
	}
}

func TestBadEntriesSayWhere(t *testing.T) {
	for entry, reason := range map[string]string{
		"two words":     "neither a name",
		"port:443":      "unknown kind",
		"ip:not-one":    "not an address",
		"geosite:Bad_X": "name of a list",
	} {
		_, err := Lists{Direct: []string{"ok.example", entry}, Rest: "PROXY"}.Text()
		if err == nil || !strings.Contains(err.Error(), reason) || !strings.Contains(err.Error(), "directly, entry 2") {
			t.Errorf("%q: got %v", entry, err)
		}
	}
	if _, err := (Lists{Rest: "REJECT"}).Text(); err == nil {
		t.Error("the rest cannot be refused: nothing would work")
	}
}
