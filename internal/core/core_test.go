package core

import (
	"encoding/base64"
	"testing"

	"github.com/metacubex/mihomo/adapter"

	"github.com/oviron/flclash-desktop/internal/compile"
	"github.com/oviron/flclash-desktop/internal/ingest"
)

// Every kind of server the importer writes has to be one the core takes: a
// server that imports and then refuses to start is the worst of both.
func TestTheCoreTakesWhatTheImporterWrites(t *testing.T) {
	vmess := "vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"vm","add":"v.example","port":"443","id":"11111111-2222-3333-4444-555555555555","aid":"0","net":"ws","host":"v.example","path":"/p","tls":"tls"}`))
	ss := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:password")) + "@s.example:8388#ss"
	clash, _, _ := ingest.ParseAll("proxies:\n  - {name: y, type: trojan, server: y.example, port: 443, password: pw, sni: y.example, skip-cert-verify: true}\n")
	if len(clash) != 1 {
		t.Fatal("the Clash proxy was not read")
	}
	if proxy, err := adapter.ParseProxy(compile.Proxy(clash[0])); err != nil {
		t.Errorf("the core refuses a proxy read from a Clash config: %v", err)
	} else {
		_ = proxy.Close()
	}
	xray, _, _ := ingest.ParseAll(`[{"protocol":"vless","tag":"xr","settings":{"vnext":[{"address":"x.example","port":443,"users":[{"id":"11111111-2222-3333-4444-555555555555","encryption":"none","flow":"xtls-rprx-vision"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"fingerprint":"qq","publicKey":"3Vc1vBHa3AP1gEhQb0zJdzLPpGhy2RK4n5eCoR9dSUk","serverName":"cover.example","shortId":"ab12","show":false,"spiderX":"/"}}}]`)
	if len(xray) != 1 {
		t.Fatal("the Xray outbound array was not read")
	}
	if proxy, err := adapter.ParseProxy(compile.Proxy(xray[0])); err != nil {
		t.Errorf("the core refuses a proxy read from an Xray array: %v", err)
	} else {
		_ = proxy.Close()
	}
	for _, link := range []string{
		"vless://11111111-2222-3333-4444-555555555555@a.example:443?type=tcp&security=reality&sni=cover.example&fp=chrome&pbk=3Vc1vBHa3AP1gEhQb0zJdzLPpGhy2RK4n5eCoR9dSUk&sid=ab12&flow=xtls-rprx-vision#reality",
		"vless://11111111-2222-3333-4444-555555555555@a.example:443?type=tcp&security=reality&sni=cover.example&fp=qq&pbk=3Vc1vBHa3AP1gEhQb0zJdzLPpGhy2RK4n5eCoR9dSUk&sid=ab12&flow=xtls-rprx-vision#qq",
		"vless://11111111-2222-3333-4444-555555555555@a.example:443?type=tcp&security=reality&sni=cover.example&fp=360&pbk=3Vc1vBHa3AP1gEhQb0zJdzLPpGhy2RK4n5eCoR9dSUk#360",
		"vless://11111111-2222-3333-4444-555555555555@a.example:443?type=tcp&security=reality&sni=cover.example&fp=randomized&pbk=3Vc1vBHa3AP1gEhQb0zJdzLPpGhy2RK4n5eCoR9dSUk#randomized",
		"vless://11111111-2222-3333-4444-555555555555@a.example:443?type=ws&security=tls&sni=a.example&path=%2Fws%3Fed%3D2048&host=a.example&fp=firefox#ws",
		"vless://11111111-2222-3333-4444-555555555555@a.example:443?type=grpc&security=tls&sni=a.example&serviceName=svc#grpc",
		"vless://11111111-2222-3333-4444-555555555555@a.example:443?type=xhttp&security=reality&pbk=3Vc1vBHa3AP1gEhQb0zJdzLPpGhy2RK4n5eCoR9dSUk&sni=a.example&path=%2Fx&mode=packet-up#xhttp",
		"trojan://password@tr.example:443?security=tls&sni=tr.example&type=ws&path=%2Ft#trojan",
		"hysteria2://password@h.example:8443?sni=h.example&insecure=1&obfs=salamander&obfs-password=pw#hy2",
		"tuic://11111111-2222-3333-4444-555555555555:password@t.example:443?congestion_control=bbr&udp_relay_mode=native&alpn=h3&sni=t.example#tuic",
		ss,
		vmess,
	} {
		server, err := ingest.Parse(link)
		if err != nil {
			t.Errorf("%s: %v", link, err)
			continue
		}
		proxy, err := adapter.ParseProxy(compile.Proxy(server))
		if err != nil {
			t.Errorf("the core refuses %s (%s): %v", server.Name, server.Proxy["type"], err)
			continue
		}
		_ = proxy.Close()
	}
}
