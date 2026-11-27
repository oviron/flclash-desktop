package ingest

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Parse reads one server in any form this package knows: a Throne profile or
// link, or a share link of the kind panels and subscriptions hand out.
func Parse(text string) (Server, error) {
	text = strings.TrimSpace(text)
	scheme, _, isLink := strings.Cut(text, "://")
	if !isLink {
		return FromThrone(text)
	}
	switch strings.ToLower(scheme) {
	case "throne":
		return FromThrone(text)
	case "vless":
		return fromURL(text, vlessLink)
	case "trojan":
		return fromURL(text, trojanLink)
	case "hysteria2", "hy2":
		return fromURL(text, hysteria2Link)
	case "tuic":
		return fromURL(text, tuicLink)
	case "ss":
		return shadowsocksLink(text)
	case "vmess":
		return vmessLink(text)
	}
	return Server{}, fmt.Errorf("links of the kind %s:// are not supported", scheme)
}

// IsSubscriptionBody reports whether text is what a subscription serves: one
// base64 blob holding a link per line. It returns the lines when it is.
func IsSubscriptionBody(text string) (string, bool) {
	compact := strings.Join(strings.Fields(text), "")
	if compact == "" || strings.Contains(compact, "://") || strings.HasPrefix(compact, "{") {
		return "", false
	}
	decoded, err := decodeBase64(compact)
	if err != nil || !strings.Contains(decoded, "://") {
		return "", false
	}
	return decoded, true
}

func decodeBase64(text string) (string, error) {
	text = strings.TrimRight(text, "=")
	for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		if raw, err := encoding.DecodeString(text); err == nil {
			return string(raw), nil
		}
	}
	return "", errors.New("not base64")
}

// link is a parsed share link. Like object, it remembers which parameters
// were read: one nobody asked for refuses the import.
type link struct {
	url   *url.URL
	query url.Values
	read  map[string]bool
	errs  []string
}

func (l *link) get(key string) string {
	l.read[key] = true
	return l.query.Get(key)
}

// either reads a parameter that panels spell in more than one way.
func (l *link) either(keys ...string) string {
	value := ""
	for _, key := range keys {
		if v := l.get(key); v != "" && value == "" {
			value = v
		}
	}
	return value
}

func (l *link) flag(keys ...string) bool {
	switch v := l.either(keys...); v {
	case "", "0", "false":
		return false
	case "1", "true":
		return true
	default:
		l.fail(keys[0], fmt.Sprintf("expected 0 or 1, got %q", v))
		return false
	}
}

func (l *link) list(key string) []string {
	value := l.get(key)
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func (l *link) fail(key, message string) {
	l.errs = append(l.errs, key+": "+message)
}

func (l *link) finish() error {
	var unread []string
	for key := range l.query {
		// Panels list parameters they leave empty; an empty one says nothing.
		if !l.read[key] && l.query.Get(key) != "" {
			unread = append(unread, key)
		}
	}
	sort.Strings(unread)
	problems := append([]string{}, l.errs...)
	if len(unread) > 0 {
		problems = append(problems, "unsupported parameters: "+strings.Join(unread, ", "))
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// endpoint reads the host and the port every link has.
func (l *link) endpoint(p map[string]any) {
	host := l.url.Hostname()
	port, err := strconv.Atoi(l.url.Port())
	if host == "" || err != nil || port < 1 || port > 65535 {
		l.fail("address", "the link has no host and port")
	}
	p["server"], p["port"] = host, port
}

func fromURL(text string, build func(*link) map[string]any) (Server, error) {
	u, err := url.Parse(text)
	if err != nil {
		return Server{}, fmt.Errorf("the link does not read: %w", err)
	}
	l := &link{url: u, query: u.Query(), read: map[string]bool{}}
	proxy := build(l)
	if err := l.finish(); err != nil {
		return Server{}, err
	}
	return Server{Name: linkName(u.Fragment, proxy), Proxy: proxy}, nil
}

// linkName is the name after # in a link, or the address when there is none.
func linkName(fragment string, proxy map[string]any) string {
	if name := strings.TrimSpace(fragment); name != "" {
		return name
	}
	return net.JoinHostPort(fmt.Sprint(proxy["server"]), fmt.Sprint(proxy["port"]))
}

// linkFingerprints are the uTLS names a link may carry that mean the same in
// the core; the ones in fingerprints are renamed.
var linkFingerprints = map[string]bool{"safari": true, "ios": true, "android": true, "edge": true, "random": true}

func (l *link) fingerprint() string {
	name := l.get("fp")
	if name == "" {
		return ""
	}
	if mapped, ok := fingerprints[name]; ok {
		return mapped
	}
	if linkFingerprints[name] {
		return name
	}
	l.fail("fp", fmt.Sprintf("fingerprint %q has no known counterpart in the core", name))
	return ""
}

// tls reads the TLS side of a link into a proxy whose server name key is sniKey.
func (l *link) tls(p map[string]any, sniKey string) {
	if sni := l.either("sni", "peer"); sni != "" {
		p[sniKey] = sni
	}
	if alpn := l.list("alpn"); len(alpn) > 0 {
		p["alpn"] = alpn
	}
	if fp := l.fingerprint(); fp != "" {
		p["client-fingerprint"] = fp
	}
	if l.flag("allowInsecure", "insecure", "allow_insecure") {
		p["skip-cert-verify"] = true
	}
}

// transport reads how the stream is carried: plain, WebSocket, gRPC or XHTTP.
func (l *link) transport(p map[string]any, xhttp bool) {
	if header := l.get("headerType"); header != "" && header != "none" {
		l.fail("headerType", fmt.Sprintf("header type %q is not supported", header))
	}
	switch network := l.get("type"); network {
	case "", "tcp", "raw":
		p["network"] = "tcp"
	case "ws":
		p["network"] = "ws"
		opts := map[string]any{}
		path := l.get("path")
		// Xray writes the early data size into the path.
		if before, ed, found := strings.Cut(path, "?ed="); found {
			size, err := strconv.Atoi(ed)
			if err != nil {
				l.fail("path", "the early data size after ?ed= is not a number")
			}
			path = before
			opts["max-early-data"] = size
			opts["early-data-header-name"] = "Sec-WebSocket-Protocol"
		}
		if path == "" {
			path = "/"
		}
		opts["path"] = path
		if host := l.get("host"); host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		p["ws-opts"] = opts
	case "grpc":
		p["network"] = "grpc"
		if mode := l.get("mode"); mode != "" && mode != "gun" {
			l.fail("mode", fmt.Sprintf("gRPC mode %q is not supported", mode))
		}
		p["grpc-opts"] = map[string]any{"grpc-service-name": l.get("serviceName")}
	case "xhttp":
		if !xhttp {
			l.fail("type", "XHTTP is supported for VLESS only")
			return
		}
		p["network"] = "xhttp"
		fields := map[string]any{}
		for _, key := range []string{"path", "host", "mode"} {
			if value := l.get(key); value != "" {
				fields[key] = value
			}
		}
		if extra := l.get("extra"); extra != "" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(extra), &parsed); err != nil {
				l.fail("extra", "is not a JSON object")
			} else {
				fields["extra"] = parsed
			}
		}
		// The same reading as for a Throne profile, with the same strictness.
		x := newObject("xhttp", fields)
		p["xhttp-opts"] = xhttpOpts(x)
		if err := x.finish(); err != nil {
			l.errs = append(l.errs, err.Error())
		}
	default:
		l.fail("type", fmt.Sprintf("transport %q is not supported", network))
	}
}

func vlessLink(l *link) map[string]any {
	p := map[string]any{"type": "vless", "udp": true, "packet-encoding": "xudp", "uuid": l.url.User.Username()}
	l.endpoint(p)
	if p["uuid"] == "" {
		l.fail("user", "the link has no UUID")
	}
	if flow := l.get("flow"); flow != "" {
		p["flow"] = flow
	}
	if enc := l.get("encryption"); enc != "" && enc != "none" {
		p["encryption"] = enc
	}
	switch encoding := l.get("packetEncoding"); encoding {
	case "", "xudp":
	case "packetaddr":
		p["packet-encoding"] = encoding
	case "none":
		delete(p, "packet-encoding")
	default:
		l.fail("packetEncoding", fmt.Sprintf("packet encoding %q is not supported", encoding))
	}
	switch security := l.get("security"); security {
	case "reality":
		p["tls"] = true
		if sni := l.either("sni", "peer"); sni != "" {
			p["servername"] = sni
		}
		// Xray speaks as Chrome when a REALITY link names nobody.
		fp := l.fingerprint()
		if fp == "" {
			fp = fingerprints["chrome"]
		}
		p["client-fingerprint"] = fp
		opts := map[string]any{"public-key": l.get("pbk")}
		if opts["public-key"] == "" {
			l.fail("pbk", "a REALITY link needs the public key")
		}
		if sid := l.get("sid"); sid != "" {
			opts["short-id"] = sid
		}
		if carriesMLKEM[fp] {
			opts["support-x25519mlkem768"] = true
		}
		p["reality-opts"] = opts
		// The path of the first request to the cover site; the core picks its own.
		l.get("spx")
		// REALITY checks the server by its key, whatever a link says about certificates.
		l.flag("allowInsecure", "insecure", "allow_insecure")
	case "tls":
		p["tls"] = true
		l.tls(p, "servername")
	case "", "none":
	default:
		l.fail("security", fmt.Sprintf("security %q is not supported", security))
	}
	l.transport(p, true)
	return p
}

func trojanLink(l *link) map[string]any {
	p := map[string]any{"type": "trojan", "udp": true, "password": l.url.User.Username()}
	l.endpoint(p)
	if p["password"] == "" {
		l.fail("user", "the link has no password")
	}
	if security := l.get("security"); security != "" && security != "tls" {
		l.fail("security", fmt.Sprintf("security %q is not supported for Trojan", security))
	}
	l.tls(p, "sni")
	l.transport(p, false)
	return p
}

func hysteria2Link(l *link) map[string]any {
	// The password may be written as user:password; the server sees it whole.
	password := l.url.User.Username()
	if second, has := l.url.User.Password(); has {
		password += ":" + second
	}
	p := map[string]any{"type": "hysteria2", "password": password}
	l.endpoint(p)
	if password == "" {
		l.fail("user", "the link has no password")
	}
	l.tls(p, "sni")
	if obfs := l.get("obfs"); obfs != "" && obfs != "none" {
		if obfs != "salamander" {
			l.fail("obfs", fmt.Sprintf("obfuscation %q is not supported", obfs))
		}
		p["obfs"] = obfs
		p["obfs-password"] = l.get("obfs-password")
	}
	return p
}

func tuicLink(l *link) map[string]any {
	password, _ := l.url.User.Password()
	p := map[string]any{"type": "tuic", "uuid": l.url.User.Username(), "password": password}
	l.endpoint(p)
	if p["uuid"] == "" {
		l.fail("user", "the link has no UUID")
	}
	if cc := l.either("congestion_control", "congestion-control"); cc != "" {
		p["congestion-controller"] = cc
	}
	if mode := l.either("udp_relay_mode", "udp-relay-mode"); mode != "" {
		p["udp-relay-mode"] = mode
	}
	l.tls(p, "sni")
	return p
}

// shadowsocksLink reads ss://, in the form with the method and password in
// base64 before the address (SIP002) and in the older one with everything in
// base64.
func shadowsocksLink(text string) (Server, error) {
	body := text[len("ss://"):]
	body, fragment, _ := strings.Cut(body, "#")
	name, err := url.PathUnescape(fragment)
	if err != nil {
		name = fragment
	}
	body, query, _ := strings.Cut(body, "?")
	if query != "" {
		values, _ := url.ParseQuery(query)
		if values.Get("plugin") != "" {
			return Server{}, errors.New("plugin: Shadowsocks plugins are not supported")
		}
		if len(values) > 0 {
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			return Server{}, errors.New("unsupported parameters: " + strings.Join(keys, ", "))
		}
	}
	credentials, address, split := cutLast(body, "@")
	if !split {
		// Everything is in base64: method:password@host:port.
		decoded, err := decodeBase64(body)
		if err != nil {
			return Server{}, errors.New("the link is neither SIP002 nor base64")
		}
		credentials, address, split = cutLast(decoded, "@")
		if !split {
			return Server{}, errors.New("the link has no address")
		}
	} else if decoded, err := decodeBase64(credentials); err == nil && strings.Contains(decoded, ":") {
		credentials = decoded
	} else if unescaped, err := url.PathUnescape(credentials); err == nil {
		credentials = unescaped
	}
	method, password, ok := strings.Cut(credentials, ":")
	if !ok || method == "" || password == "" {
		return Server{}, errors.New("the link has no method and password")
	}
	host, portText, err := net.SplitHostPort(strings.TrimSuffix(address, "/"))
	port, portErr := strconv.Atoi(portText)
	if err != nil || portErr != nil || host == "" {
		return Server{}, errors.New("the link has no host and port")
	}
	proxy := map[string]any{"type": "ss", "server": host, "port": port, "cipher": method, "password": password, "udp": true}
	return Server{Name: linkName(name, proxy), Proxy: proxy}, nil
}

func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

// vmessLink reads vmess://, a base64 JSON object in the form V2RayN set.
func vmessLink(text string) (Server, error) {
	decoded, err := decodeBase64(strings.TrimSpace(text[len("vmess://"):]))
	if err != nil {
		return Server{}, errors.New("the link payload is not base64")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(decoded), &fields); err != nil {
		return Server{}, fmt.Errorf("the link payload is not JSON: %w", err)
	}
	o := newObject("", fields)
	// Panels write numbers as strings and as numbers alike.
	text2 := func(key string) string {
		v, ok := o.get(key)
		if !ok {
			return ""
		}
		switch t := v.(type) {
		case string:
			return t
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64)
		}
		o.fail(key, "expected a plain value")
		return ""
	}
	number := func(key string) int {
		value := text2(key)
		if value == "" {
			return 0
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			o.fail(key, "expected a whole number")
		}
		return n
	}
	text2("v")
	cipher := text2("scy")
	if cipher == "" {
		cipher = "auto"
	}
	p := map[string]any{
		"type": "vmess", "udp": true,
		"server": text2("add"), "port": number("port"),
		"uuid": text2("id"), "alterId": number("aid"), "cipher": cipher,
	}
	if header := text2("type"); header != "" && header != "none" {
		o.fail("type", fmt.Sprintf("header type %q is not supported", header))
	}
	host, path := text2("host"), text2("path")
	switch network := text2("net"); network {
	case "", "tcp", "raw":
		p["network"] = "tcp"
	case "ws":
		p["network"] = "ws"
		if path == "" {
			path = "/"
		}
		opts := map[string]any{"path": path}
		if host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		p["ws-opts"] = opts
	case "grpc":
		p["network"] = "grpc"
		p["grpc-opts"] = map[string]any{"grpc-service-name": path}
	default:
		o.fail("net", fmt.Sprintf("transport %q is not supported", network))
	}
	switch security := text2("tls"); security {
	case "", "none":
	case "tls":
		p["tls"] = true
		sni := text2("sni")
		if sni == "" {
			sni = host
		}
		if sni != "" {
			p["servername"] = sni
		}
		if alpn := text2("alpn"); alpn != "" {
			p["alpn"] = strings.Split(alpn, ",")
		}
		if fp := text2("fp"); fp != "" {
			mapped, known := fingerprints[fp]
			if !known && linkFingerprints[fp] {
				mapped, known = fp, true
			}
			if !known {
				o.fail("fp", fmt.Sprintf("fingerprint %q has no known counterpart in the core", fp))
			}
			p["client-fingerprint"] = mapped
		}
	default:
		o.fail("tls", fmt.Sprintf("security %q is not supported", security))
	}
	name := text2("ps")
	if err := o.finish(); err != nil {
		return Server{}, err
	}
	if p["server"] == "" || p["uuid"] == "" {
		return Server{}, errors.New("the link has no address or no UUID")
	}
	return Server{Name: linkName(name, p), Proxy: p}, nil
}
