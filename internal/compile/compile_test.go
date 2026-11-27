package compile

import (
	"strings"
	"testing"

	"github.com/metacubex/mihomo/config"
	// config links to a function that only this package defines.
	_ "github.com/metacubex/mihomo/hub/executor"

	"github.com/oviron/flclash-desktop/internal/ingest"
	"github.com/oviron/flclash-desktop/internal/policy"
)

func server(t *testing.T) ingest.Server {
	t.Helper()
	s, err := ingest.FromThrone(`{"type":"hysteria2","tag":"nl-hy2","server":"a.example","server_port":443,"password":"pw",
	  "tls":{"enabled":true,"server_name":"a.example","alpn":["h3"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The compiled config has to be one the core itself accepts, with the policy's
// PROXY bound to the selected server.
func TestCoreAcceptsTheConfig(t *testing.T) {
	for _, name := range policy.BuiltinNames() {
		pol, _ := policy.Builtin(name)
		raw, err := Config(Input{Server: server(t), Policy: pol, LocalPort: 17890})
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := config.Parse(raw)
		if err != nil {
			t.Fatalf("policy %s: the core refused the config: %v\n%s", name, err, raw)
		}
		group, ok := parsed.Proxies[policy.Proxy]
		if !ok {
			t.Fatalf("policy %s: the config has no PROXY group", name)
		}
		if described, _ := group.MarshalJSON(); !strings.Contains(string(described), `"now":"nl-hy2"`) {
			t.Fatalf("policy %s: PROXY does not lead to the server: %s", name, described)
		}
		if len(parsed.Rules) != len(pol.Rules) {
			t.Fatalf("policy %s: %d rules in, %d in the core", name, len(pol.Rules), len(parsed.Rules))
		}
		if parsed.General.MixedPort != 17890 || parsed.General.BindAddress != "127.0.0.1" || parsed.General.AllowLan {
			t.Fatalf("policy %s: the test listener is not loopback-only", name)
		}
	}
}

func TestReservedNamesAndMissingChoice(t *testing.T) {
	pol, _ := policy.Builtin("all")
	s := server(t)
	s.Name = "DIRECT"
	if _, err := Config(Input{Server: s, Policy: pol, LocalPort: 17890}); err == nil || !strings.Contains(err.Error(), "cannot be named") {
		t.Fatalf("got %v", err)
	}
	if _, err := Config(Input{Policy: pol}); err == nil {
		t.Fatal("a config without a server must be refused")
	}
	if _, err := Config(Input{Server: server(t)}); err == nil {
		t.Fatal("a config without a policy must be refused")
	}
}

func TestCoreAcceptsTheTUNConfig(t *testing.T) {
	pol, _ := policy.Builtin("lan-direct")
	for _, os := range []string{"darwin", "windows", "linux"} {
		raw, err := Config(Input{Server: server(t), Policy: pol, TUN: true, OS: os})
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := config.Parse(raw)
		if err != nil {
			t.Fatalf("%s: the core refused the config: %v\n%s", os, err, raw)
		}
		tun := parsed.General.Tun
		if !tun.Enable || !tun.AutoRoute || !tun.AutoDetectInterface || len(tun.DNSHijack) != 2 {
			t.Fatalf("%s: TUN is not set up to capture: %+v", os, tun)
		}
		if native := tun.DNSMode == "native"; native != (os == "darwin") {
			t.Fatalf("%s: dns-mode is %q", os, tun.DNSMode)
		}
		if parsed.General.MixedPort != 0 || parsed.General.IPv6 || !parsed.DNS.Enable {
			t.Fatalf("%s: a TUN config opens no local port, keeps IPv6 off and runs DNS", os)
		}
	}
}

func TestAConfigMustCaptureSomething(t *testing.T) {
	pol, _ := policy.Builtin("all")
	if _, err := Config(Input{Server: server(t), Policy: pol}); err == nil {
		t.Fatal("a config with neither TUN nor a local port must be refused")
	}
}

func TestListRulesBecomeRuleSets(t *testing.T) {
	pol, err := policy.Parse("mine", "GEOSITE,category-ru,DIRECT\nGEOIP,ru,DIRECT\nDOMAIN,a.example,REJECT\nMATCH,PROXY\n")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Config(Input{Server: server(t), Policy: pol, TUN: true, OS: "darwin"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"RULE-SET,geosite-category-ru,DIRECT",
		"RULE-SET,geoip-ru,DIRECT,no-resolve",
		"geo/geosite/category-ru.mrs",
		"geo/geoip/ru.mrs",
		"./ruleset/geosite-category-ru.mrs",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the config lacks %q", want)
		}
	}
	parsed, err := config.Parse(raw)
	if err != nil {
		t.Fatalf("the core refused the config: %v", err)
	}
	if len(parsed.RuleProviders) != 2 || len(parsed.Rules) != 4 {
		t.Fatalf("got %d providers and %d rules", len(parsed.RuleProviders), len(parsed.Rules))
	}
}
