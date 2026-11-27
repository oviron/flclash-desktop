package ingest

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

const (
	uuid = "11111111-2222-3333-4444-555555555555"
	key  = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

func mustImport(t *testing.T, text string) Server {
	t.Helper()
	s, err := FromThrone(text)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	return s
}

func wantError(t *testing.T, text, part string) {
	t.Helper()
	_, err := FromThrone(text)
	if err == nil || !strings.Contains(err.Error(), part) {
		t.Fatalf("want an error mentioning %q, got %v", part, err)
	}
}

func TestRealityXHTTPWithoutXmuxGetsXrayPoolDefaults(t *testing.T) {
	s := mustImport(t, `{"protocol":"vless","tag":"nl-xhttp",
	  "settings":{"address":"a.example","port":443,"id":"`+uuid+`","encryption":"none"},
	  "streamSettings":{"network":"xhttp","security":"reality",
	    "realitySettings":{"fingerprint":"firefox","password":"`+key+`","serverName":"a.example","shortId":"0123456789ab"},
	    "xhttpSettings":{"path":"/p","mode":"auto","extra":{"mode":"auto","xPaddingBytes":"100-1000"}}}}`)
	want := map[string]any{
		"type": "vless", "server": "a.example", "port": 443, "uuid": uuid,
		"udp": true, "packet-encoding": "xudp", "tls": true,
		"servername": "a.example", "client-fingerprint": "firefox",
		"reality-opts": map[string]any{"public-key": key, "short-id": "0123456789ab", "support-x25519mlkem768": true},
		"network":      "xhttp",
		"xhttp-opts": map[string]any{
			"path": "/p", "mode": "auto", "x-padding-bytes": "100-1000",
			"reuse-settings": map[string]any{
				"max-concurrency": "16-32", "max-connections": "0", "c-max-reuse-times": "0",
				"h-max-request-times": "600-900", "h-max-reusable-secs": "1800-3000", "h-keep-alive-period": 0,
			},
		},
	}
	if s.Name != "nl-xhttp" || !reflect.DeepEqual(s.Proxy, want) {
		t.Fatalf("got %s %#v", s.Name, s.Proxy)
	}
}

func TestExplicitXmuxReplacesTheDefaults(t *testing.T) {
	s := mustImport(t, `{"protocol":"vless","tag":"nl-up",
	  "settings":{"address":"a.example","port":443,"id":"`+uuid+`","encryption":"none"},
	  "streamSettings":{"network":"xhttp","security":"reality",
	    "realitySettings":{"fingerprint":"firefox","password":"`+key+`","serverName":"a.example","shortId":"ab"},
	    "xhttpSettings":{"path":"/p","mode":"stream-up","extra":{"mode":"stream-up","xmux":{"hKeepAlivePeriod":15,"maxConnections":"1"}}}}}`)
	got := s.Proxy["xhttp-opts"].(map[string]any)["reuse-settings"]
	want := map[string]any{"max-connections": "1", "h-keep-alive-period": 15}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

const cdnProfile = `{"protocol":"vless","tag":"cdn",
  "settings":{"address":"cdn.example","port":443,"id":"` + uuid + `","encryption":"mlkem768x25519plus.native.0rtt.KEY"},
  "streamSettings":{"network":"xhttp","security":"tls",
    "tlsSettings":{"alpn":["h2","http/1.1"],"fingerprint":"firefox","serverName":"front.example","verifyPeerCertByName":"NAMES"},
    "xhttpSettings":{"host":"cdn.example","mode":"packet-up","path":"/up/",
      "extra":{"host":"cdn.example","mode":"packet-up","scMaxEachPostBytes":"16384","scMinPostsIntervalMs":"30",
        "uplinkDataKey":"X-Part","uplinkDataPlacement":"header","uplinkHTTPMethod":"GET",
        "xPaddingBytes":"100-1000","xPaddingHeader":"X-Token","xPaddingKey":"_t","xPaddingMethod":"tokenish",
        "xPaddingObfsMode":true,"xPaddingPlacement":"queryInHeader"}}}}`

func TestEncryptionOverPacketUp(t *testing.T) {
	s := mustImport(t, strings.Replace(cdnProfile, "NAMES", "edge.example", 1))
	if s.Proxy["encryption"] != "mlkem768x25519plus.native.0rtt.KEY" || s.Proxy["name-cert-verify"] != "edge.example" {
		t.Fatalf("got %#v", s.Proxy)
	}
	if _, isReality := s.Proxy["reality-opts"]; isReality {
		t.Fatal("a TLS profile must not get reality-opts")
	}
	x := s.Proxy["xhttp-opts"].(map[string]any)
	for key, want := range map[string]any{
		"host": "cdn.example", "mode": "packet-up", "path": "/up/",
		"sc-max-each-post-bytes": "16384", "sc-min-posts-interval-ms": "30",
		"uplink-data-key": "X-Part", "uplink-data-placement": "header", "uplink-http-method": "GET",
		"x-padding-header": "X-Token", "x-padding-key": "_t", "x-padding-method": "tokenish",
		"x-padding-obfs-mode": true, "x-padding-placement": "queryInHeader",
	} {
		if x[key] != want {
			t.Errorf("xhttp-opts.%s = %#v, want %#v", key, x[key], want)
		}
	}
}

func TestSeveralCertificateNamesAreRefused(t *testing.T) {
	wantError(t, strings.Replace(cdnProfile, "NAMES", "a.example,b.example", 1), "one name")
}

func TestVisionKeepsFirefox120(t *testing.T) {
	s := mustImport(t, `{"protocol":"vless","tag":"de-vision",
	  "settings":{"address":"r.example","port":443,"id":"`+uuid+`","encryption":"none","flow":"xtls-rprx-vision"},
	  "streamSettings":{"network":"raw","security":"tls",
	    "tlsSettings":{"alpn":["http/1.1"],"fingerprint":"hellofirefox_120","serverName":"r.example"}}}`)
	if s.Proxy["client-fingerprint"] != "firefox120" || s.Proxy["network"] != "tcp" || s.Proxy["flow"] != "xtls-rprx-vision" {
		t.Fatalf("got %#v", s.Proxy)
	}
}

func TestXrayChromeIsChrome155(t *testing.T) {
	s := mustImport(t, `{"protocol":"vless","tag":"tr-grpc",
	  "settings":{"address":"t.example","port":2053,"id":"`+uuid+`","encryption":"none"},
	  "streamSettings":{"network":"grpc","security":"reality","grpcSettings":{"serviceName":"svc"},
	    "realitySettings":{"fingerprint":"chrome","password":"`+key+`","serverName":"t.example","shortId":"ab"}}}`)
	if s.Proxy["client-fingerprint"] != "chrome155" {
		t.Fatalf("got %v", s.Proxy["client-fingerprint"])
	}
	if !reflect.DeepEqual(s.Proxy["grpc-opts"], map[string]any{"grpc-service-name": "svc"}) {
		t.Fatalf("got %#v", s.Proxy["grpc-opts"])
	}
}

func TestTuicAndHysteria2(t *testing.T) {
	tuic := mustImport(t, `{"type":"tuic","tag":"nl-tuic","server":"a.example","server_port":2052,"uuid":"`+uuid+`","password":"pw",
	  "congestion_control":"bbr","udp_relay_mode":"native","tls":{"enabled":true,"server_name":"a.example","alpn":["h3"]}}`)
	wantTuic := map[string]any{"type": "tuic", "server": "a.example", "port": 2052, "uuid": uuid, "password": "pw",
		"congestion-controller": "bbr", "udp-relay-mode": "native", "sni": "a.example", "alpn": []string{"h3"}}
	if !reflect.DeepEqual(tuic.Proxy, wantTuic) {
		t.Fatalf("got %#v", tuic.Proxy)
	}
	hy2 := mustImport(t, `{"type":"hysteria2","tag":"nl-hy2","server":"a.example","server_port":2087,"password":"pw",
	  "tls":{"enabled":true,"server_name":"a.example","alpn":["h3"]}}`)
	wantHy2 := map[string]any{"type": "hysteria2", "server": "a.example", "port": 2087, "password": "pw",
		"sni": "a.example", "alpn": []string{"h3"}}
	if !reflect.DeepEqual(hy2.Proxy, wantHy2) {
		t.Fatalf("got %#v", hy2.Proxy)
	}
}

func TestLinkIsTheSameProfile(t *testing.T) {
	profile := `{"type":"hysteria2","tag":"h","server":"a.example","server_port":443,"password":"pw","tls":{"enabled":true,"server_name":"a.example"}}`
	link := "throne://add/" + base64.RawURLEncoding.EncodeToString([]byte(profile))
	if !reflect.DeepEqual(mustImport(t, link), mustImport(t, profile)) {
		t.Fatal("the link and the JSON import differently")
	}
}

func TestWhatIsNotMappedIsRefused(t *testing.T) {
	base := `{"type":"hysteria2","tag":"h","server":"a.example","server_port":443,"password":"pw","tls":{"enabled":true,"server_name":"a.example"}`
	wantError(t, base+`,"obfs":{"type":"salamander","password":"x"}}`, "unsupported fields: obfs")
	wantError(t, strings.Replace(base, `"enabled":true`, `"enabled":true,"insecure":true`, 1)+`}`, "tls.insecure")
	wantError(t, `{"type":"wireguard","tag":"w"}`, `"wireguard" is not supported`)
	wantError(t, `{"protocol":"vless","tag":"x","settings":{"address":"a.example","port":443,"id":"`+uuid+`"},
	  "streamSettings":{"network":"raw","security":"tls","tlsSettings":{"fingerprint":"safari","serverName":"a.example"}}}`, `fingerprint "safari"`)
	wantError(t, "throne://add/%%%", "not base64")
}
