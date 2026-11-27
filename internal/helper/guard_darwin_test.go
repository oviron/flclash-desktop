package helper

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pfctl -n reads rules without loading them, and needs no privileges.
func TestTheFirewallAcceptsTheRules(t *testing.T) {
	for name, rules := range map[string]string{
		"full":         pfRules("utun9", CoreGID, true, []string{"198.19.248.0/22"}),
		"full, no tun": pfRules("", CoreGID, true, nil),
		"ipv6":         pfRules("utun9", CoreGID, false, nil),
	} {
		file := filepath.Join(t.TempDir(), "rules")
		if err := os.WriteFile(file, []byte(rules), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("/sbin/pfctl", "-vnf", file).CombinedOutput()
		if err != nil {
			t.Errorf("%s: %v\n%s\n%s", name, err, out, rules)
		}
		if full := strings.HasPrefix(name, "full"); full != strings.Contains(string(out), guardLabel) {
			t.Errorf("%s: the label that marks a full guard is where it should not be:\n%s", name, out)
		}
	}
}

func TestTheRulesOfAFullGuard(t *testing.T) {
	rules := pfRules("utun9", CoreGID, true, nil)
	for _, want := range []string{"on utun9", "group 49251", "port { 53, 853 }", "192.168.0.0/16", "block return out all"} {
		if !strings.Contains(rules, want) {
			t.Errorf("no %q in:\n%s", want, rules)
		}
	}
	// Without the full guard IPv4 is left alone and the device is not named.
	loose := pfRules("utun9", CoreGID, false, nil)
	if strings.Contains(loose, "utun9") || strings.Contains(loose, "block return out all") || !strings.Contains(loose, "block return out inet6 all") {
		t.Errorf("got:\n%s", loose)
	}
}
