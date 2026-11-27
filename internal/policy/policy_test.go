package policy

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseNormalisesRules(t *testing.T) {
	p, err := Parse("mine", `
# work
domain-suffix, Example.COM , direct
IP-CIDR,10.1.2.3/8,DIRECT   # masked to the network
DOMAIN-KEYWORD,ads,REJECT
MATCH,PROXY
`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"DOMAIN-SUFFIX,example.com,DIRECT",
		"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
		"DOMAIN-KEYWORD,ads,REJECT",
		"MATCH,PROXY",
	}
	if !reflect.DeepEqual(p.Rules, want) {
		t.Fatalf("got %#v", p.Rules)
	}
}

func TestErrorsNameTheLine(t *testing.T) {
	for text, part := range map[string]string{
		"":                                     "empty",
		"DOMAIN,a.example,PROXY\n":             "line 1: the last rule must be MATCH",
		"MATCH,PROXY\nDOMAIN,a,DIRECT\n":       "line 2: a rule after MATCH",
		"\nDOMAIN,a.example,vpn\nMATCH,PROXY":  "line 2: unknown destination",
		"GEOIP,RU,DIRECT\nMATCH,PROXY":         "line 1: rule type \"GEOIP\" is not supported",
		"IP-CIDR,fc00::/7,DIRECT\nMATCH,PROXY": "line 1: fc00::/7 does not fit IP-CIDR",
		"IP-CIDR,10.0.0.0,DIRECT\nMATCH,PROXY": "line 1: \"10.0.0.0\" is not a network",
	} {
		_, err := Parse("p", text)
		if err == nil || !strings.Contains(err.Error(), part) {
			t.Errorf("%q: want an error mentioning %q, got %v", text, part, err)
		}
	}
}

func TestBuiltinsParse(t *testing.T) {
	for _, name := range BuiltinNames() {
		p, ok := Builtin(name)
		if !ok || p.Rules[len(p.Rules)-1] != "MATCH,PROXY" {
			t.Errorf("built-in %q: %v %v", name, ok, p.Rules)
		}
	}
}
