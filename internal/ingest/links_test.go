package ingest

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/oviron/flclash-desktop/internal/say"
)

func mustParse(t *testing.T, link string) Server {
	t.Helper()
	s, err := Parse(link)
	if err != nil {
		t.Fatalf("%s: %v", link, err)
	}
	if err := Check(s); err != nil {
		t.Fatalf("the helper would refuse what Parse made of %s: %v", link, err)
	}
	return s
}

func TestVlessLinks(t *testing.T) {
	s := mustParse(t, "vless://11111111-2222-3333-4444-555555555555@a.example:443?type=tcp&security=reality&sni=cover.example&fp=chrome&pbk=KEY&sid=ab12&flow=xtls-rprx-vision&spx=%2F&headerType=none&host=#My%20server")
	want := map[string]any{
		"type": "vless", "udp": true, "packet-encoding": "xudp", "network": "tcp",
		"server": "a.example", "port": 443, "uuid": "11111111-2222-3333-4444-555555555555",
		"flow": "xtls-rprx-vision", "tls": true, "servername": "cover.example", "client-fingerprint": "chrome155",
		"reality-opts": map[string]any{"public-key": "KEY", "short-id": "ab12", "support-x25519mlkem768": true},
	}
	if s.Name != "My server" || !reflect.DeepEqual(s.Proxy, want) {
		t.Fatalf("got %q %#v", s.Name, s.Proxy)
	}

	ws := mustParse(t, "vless://u@b.example:8443?type=ws&security=tls&sni=b.example&path=%2Fws%3Fed%3D2048&host=cdn.example&alpn=h2,http/1.1&allowInsecure=1")
	opts := ws.Proxy["ws-opts"].(map[string]any)
	if ws.Name != "b.example:8443" || opts["path"] != "/ws" || opts["max-early-data"] != 2048 ||
		opts["headers"].(map[string]any)["Host"] != "cdn.example" || ws.Proxy["skip-cert-verify"] != true ||
		!reflect.DeepEqual(ws.Proxy["alpn"], []string{"h2", "http/1.1"}) || ws.Proxy["client-fingerprint"] != nil {
		t.Fatalf("got %#v", ws.Proxy)
	}

	x := mustParse(t, `vless://u@c.example:443?type=xhttp&security=reality&pbk=K&sni=c.example&path=%2Fx&mode=packet-up&extra=%7B%22xmux%22%3A%7B%22maxConcurrency%22%3A%221%22%7D%7D#x`)
	xo := x.Proxy["xhttp-opts"].(map[string]any)
	if xo["path"] != "/x" || xo["mode"] != "packet-up" || !reflect.DeepEqual(xo["reuse-settings"], map[string]any{"max-concurrency": "1"}) {
		t.Fatalf("got %#v", xo)
	}
	for _, fp := range []string{"qq", "360", "randomized"} {
		q := mustParse(t, "vless://u@d.example:443?security=reality&pbk=K&sni=d.example&fp="+fp+"#"+fp)
		if q.Proxy["client-fingerprint"] != fp {
			t.Fatalf("fp=%s: got %#v", fp, q.Proxy["client-fingerprint"])
		}
	}
}

func TestOtherLinks(t *testing.T) {
	h := mustParse(t, "hy2://secret@h.example:8443?sni=h.example&insecure=1&obfs=salamander&obfs-password=pw#hy")
	if !reflect.DeepEqual(h.Proxy, map[string]any{
		"type": "hysteria2", "server": "h.example", "port": 8443, "password": "secret",
		"sni": "h.example", "skip-cert-verify": true, "obfs": "salamander", "obfs-password": "pw",
	}) {
		t.Fatalf("got %#v", h.Proxy)
	}
	tu := mustParse(t, "tuic://uuid-1:pass@t.example:443?congestion_control=bbr&udp_relay_mode=native&alpn=h3&sni=t.example#tuic")
	if tu.Proxy["uuid"] != "uuid-1" || tu.Proxy["password"] != "pass" || tu.Proxy["congestion-controller"] != "bbr" || tu.Proxy["udp-relay-mode"] != "native" {
		t.Fatalf("got %#v", tu.Proxy)
	}
	tr := mustParse(t, "trojan://pw@tr.example:443?security=tls&type=grpc&serviceName=svc&sni=tr.example#tr")
	if tr.Proxy["type"] != "trojan" || tr.Proxy["password"] != "pw" || tr.Proxy["sni"] != "tr.example" ||
		tr.Proxy["grpc-opts"].(map[string]any)["grpc-service-name"] != "svc" {
		t.Fatalf("got %#v", tr.Proxy)
	}

	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for _, link := range []string{
		"ss://" + enc("aes-256-gcm:pw:with:colons") + "@s.example:8388#ss",
		"ss://" + enc("aes-256-gcm:pw:with:colons@s.example:8388") + "#ss",
	} {
		ss := mustParse(t, link)
		if !reflect.DeepEqual(ss.Proxy, map[string]any{"type": "ss", "server": "s.example", "port": 8388, "cipher": "aes-256-gcm", "password": "pw:with:colons", "udp": true}) || ss.Name != "ss" {
			t.Fatalf("%s: got %q %#v", link, ss.Name, ss.Proxy)
		}
	}

	vm := mustParse(t, "vmess://"+base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"vm","add":"v.example","port":"443","id":"uuid-2","aid":0,"scy":"","net":"ws","type":"none","host":"v.example","path":"/p","tls":"tls","sni":""}`)))
	if vm.Name != "vm" || vm.Proxy["port"] != 443 || vm.Proxy["alterId"] != 0 || vm.Proxy["cipher"] != "auto" ||
		vm.Proxy["servername"] != "v.example" || vm.Proxy["ws-opts"].(map[string]any)["path"] != "/p" {
		t.Fatalf("got %#v", vm.Proxy)
	}
}

func TestLinksThatAreRefused(t *testing.T) {
	for link, reason := range map[string]string{
		"vless://u@a.example:443?security=reality&pbk=K&fragment=1,2,3":                                       "fragment",
		"vless://u@a.example:443?security=reality&pbk=K&fp=opera":                                             "fingerprint",
		"vless://u@a.example:443?security=reality":                                                            "public key",
		"vless://u@a.example:443?type=kcp":                                                                    "transport",
		"vless://u@a.example":                                                                                 "host and port",
		"trojan://pw@a.example:443?type=xhttp":                                                                "VLESS only",
		"hysteria2://pw@a.example:443?pinSHA256=abc":                                                          "pinSHA256",
		"ss://YWVzLTI1Ni1nY206cHc@a.example:1?plugin=obfs-local":                                              "plugin",
		"wireguard://key@a.example:51820":                                                                     "not supported",
		"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"a","port":"1","id":"u","net":"kcp"}`)): "transport",
	} {
		_, err := Parse(link)
		if err == nil || !strings.Contains(err.Error(), reason) {
			t.Errorf("%s: got %v, want a refusal that says %q", link, err, reason)
		}
	}
}

func TestSubscriptionBody(t *testing.T) {
	lines := "vless://u@a.example:443?security=reality&pbk=K#a\nhy2://p@b.example:443#b\n"
	body := base64.StdEncoding.EncodeToString([]byte(lines))
	got, ok := IsSubscriptionBody(body[:20] + "\n" + body[20:])
	if !ok || got != lines {
		t.Fatalf("got %q, %v", got, ok)
	}
	for _, text := range []string{lines, `{"type":"tuic"}`, "throne://add/abc", "just words"} {
		if _, ok := IsSubscriptionBody(text); ok {
			t.Errorf("%q is not a subscription body", text)
		}
	}
}

func TestClashConfig(t *testing.T) {
	servers, refused, n := ParseAll(`mixed-port: 7890
proxies:
  - name: "one"
    type: vless
    server: a.example
    port: 443
    uuid: 11111111-2222-3333-4444-555555555555
    tls: true
    servername: a.example
    network: ws
    ws-opts:
      path: /ws
      headers:
        Host: a.example
  - {name: two, type: ss, server: b.example, port: 8388, cipher: aes-256-gcm, password: pw, udp: true}
  - {name: three, type: ssh, server: c.example, port: 22}
  - {name: four, type: trojan, server: d.example, port: 443, password: pw, dialer-proxy: one}
proxy-groups: []
`)
	if n != 4 || len(servers) != 2 || len(refused) != 2 {
		t.Fatalf("got %d entries, %d servers, %+v", n, len(servers), refused)
	}
	if servers[0].Name != "one" || servers[0].Proxy["port"] != 443 || servers[0].Proxy["name"] != nil ||
		servers[0].Proxy["ws-opts"].(map[string]any)["headers"].(map[string]any)["Host"] != "a.example" {
		t.Fatalf("got %#v", servers[0])
	}
	if refused[0].Entry != 3 || !strings.Contains(refused[0].Reason, "ssh") || refused[1].Entry != 4 || !strings.Contains(refused[1].Reason, "dialer-proxy") {
		t.Fatalf("got %+v", refused)
	}
	// Links stay links.
	if _, _, n := ParseAll("hy2://p@b.example:443#b\n\nvless://u@a.example:443?security=reality&pbk=K#a\n"); n != 2 {
		t.Fatalf("got %d", n)
	}
}

// A refusal reads in the language of the app, field by field.
func TestRefusalInRussian(t *testing.T) {
	say.SetLanguage("ru")
	defer say.SetLanguage("")
	_, refused, _ := ParseAll("vless://11111111-2222-3333-4444-555555555555@example.com:443?type=quic&foo=1#x")
	if len(refused) != 1 {
		t.Fatalf("refused: %v", refused)
	}
	reason := refused[0].Reason
	if !strings.Contains(reason, "транспорт «quic» не поддерживается") || !strings.Contains(reason, "неподдерживаемые параметры: foo") {
		t.Fatalf("reason: %s", reason)
	}
}
