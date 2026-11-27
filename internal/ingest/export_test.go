package ingest

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// stored is a server after a trip through the state file, where every number
// is a float and every list a list of anything.
func stored(t *testing.T, s Server) Server {
	t.Helper()
	raw, _ := json.Marshal(s)
	var out Server
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func same(a, b Server) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestLinksComeBackAsTheServerTheyWereWrittenFrom(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for _, link := range []string{
		"vless://11111111-2222-3333-4444-555555555555@a.example:443?type=tcp&security=reality&sni=cover.example&fp=chrome&pbk=KEY&sid=ab12&flow=xtls-rprx-vision#My%20server",
		"vless://u@b.example:8443?type=ws&security=tls&sni=b.example&path=%2Fws%3Fed%3D2048&host=cdn.example&alpn=h2,http/1.1&allowInsecure=1&fp=firefox#ws",
		"vless://u@c.example:443?type=grpc&security=tls&sni=c.example&serviceName=svc&packetEncoding=none#grpc",
		`vless://u@c.example:443?type=xhttp&security=reality&pbk=K&sni=c.example&path=%2Fx&mode=packet-up&host=h.example&extra=%7B%22xmux%22%3A%7B%22maxConcurrency%22%3A%221%22%7D%2C%22xPaddingBytes%22%3A%22100-1000%22%7D#x`,
		"vless://u@d.example:443?type=xhttp&security=reality&pbk=K&sni=d.example&encryption=mlkem768x25519plus.native.0rtt.KEY#defaults",
		"trojan://pw@tr.example:443?security=tls&type=grpc&serviceName=svc&sni=tr.example#tr",
		"hy2://secret@h.example:8443?sni=h.example&insecure=1&obfs=salamander&obfs-password=pw#hy",
		"hysteria2://user:pass@h.example:8443?sni=h.example#two-part",
		"tuic://uuid-1:pass@t.example:443?congestion_control=bbr&udp_relay_mode=native&alpn=h3&sni=t.example#tuic",
		"ss://" + enc("aes-256-gcm:pw:with:colons") + "@s.example:8388#ss",
		"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"vm","add":"v.example","port":"443","id":"uuid-2","aid":0,"net":"ws","type":"none","host":"v.example","path":"/p","tls":"tls","sni":"v.example","fp":"chrome"}`)),
	} {
		first, err := Parse(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		written, err := Link(stored(t, first))
		if err != nil {
			t.Errorf("%s: %v", first.Name, err)
			continue
		}
		second, err := Parse(written)
		if err != nil {
			t.Errorf("%s: the link written does not read: %v\n%s", first.Name, err, written)
			continue
		}
		if !same(first, second) {
			a, _ := json.Marshal(first)
			b, _ := json.Marshal(second)
			t.Errorf("%s came back different:\n%s\n%s\nvia %s", first.Name, a, b, written)
		}
	}
}

func TestAServerALinkCannotCarry(t *testing.T) {
	s := Server{Name: "pinned", Proxy: map[string]any{
		"type": "vless", "server": "a.example", "port": 443, "uuid": "u", "udp": true, "packet-encoding": "xudp",
		"tls": true, "servername": "a.example", "name-cert-verify": "other.example", "network": "tcp",
	}}
	_, err := Link(s)
	if err == nil || !strings.Contains(err.Error(), "name-cert-verify") {
		t.Fatalf("got %v", err)
	}
}
