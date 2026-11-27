// Package ingest turns what the user pastes into servers in mihomo's proxy shape.
package ingest

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Server is one outbound: Proxy is an entry of mihomo's `proxies` without `name`.
type Server struct {
	Name  string         `json:"name"`
	Proxy map[string]any `json:"proxy"`
}

const throneLinkPrefix = "throne://add/"

// FromThrone reads what Throne's "copy as link" or its database gives for one
// profile: a throne://add/ link or the outbound JSON itself.
//
// A field this importer does not map is an error, never a silent drop: a
// profile that connects with a setting missing is worse than one that refuses
// to import.
func FromThrone(text string) (Server, error) {
	text = strings.TrimSpace(text)
	raw := []byte(text)
	if len(text) >= len(throneLinkPrefix) && strings.EqualFold(text[:len(throneLinkPrefix)], throneLinkPrefix) {
		payload := strings.TrimRight(text[len(throneLinkPrefix):], "=")
		decoded, err := base64.RawURLEncoding.DecodeString(payload)
		if err != nil {
			// Throne also accepts the standard alphabet here.
			if decoded, err = base64.RawStdEncoding.DecodeString(payload); err != nil {
				return Server{}, errors.New("the link payload is not base64")
			}
		}
		raw = decoded
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return Server{}, fmt.Errorf("not a Throne profile: %w", err)
	}
	o := newObject("", top)
	name := o.str("tag")
	var proxy map[string]any
	switch {
	case o.has("protocol"):
		if p := o.str("protocol"); p != "vless" {
			return Server{}, fmt.Errorf("xray protocol %q is not supported", p)
		}
		proxy = xrayVless(o)
	case o.has("type"):
		switch t := o.str("type"); t {
		case "tuic":
			proxy = singTuic(o)
		case "hysteria2":
			proxy = singHysteria2(o)
		default:
			return Server{}, fmt.Errorf("profile type %q is not supported", t)
		}
	default:
		return Server{}, errors.New("not a Throne profile: neither `protocol` nor `type`")
	}
	if err := o.finish(); err != nil {
		return Server{}, err
	}
	if name == "" {
		return Server{}, errors.New("the profile has no name (`tag`)")
	}
	return Server{Name: name, Proxy: proxy}, nil
}

// fingerprints maps the uTLS names of the Xray core in the Throne fork to the
// names of the mihomo fork. They differ: Xray's `chrome` is Chrome 155 there,
// mihomo's `chrome` is Chrome 133.
var fingerprints = map[string]string{
	"firefox":          "firefox",
	"hellofirefox_120": "firefox120",
	"chrome":           "chrome155",
}

// carriesMLKEM lists the fingerprints whose ClientHello has the
// X25519MLKEM768 key share, which REALITY keeps only when asked to.
var carriesMLKEM = map[string]bool{"firefox": true, "chrome155": true}

func xrayVless(o *object) map[string]any {
	p := map[string]any{"type": "vless", "udp": true, "packet-encoding": "xudp"}

	s := o.obj("settings")
	p["server"] = s.str("address")
	p["port"] = s.num("port")
	p["uuid"] = s.str("id")
	if flow := s.str("flow"); flow != "" {
		p["flow"] = flow
	}
	if enc := s.str("encryption"); enc != "" && enc != "none" {
		p["encryption"] = enc
	}

	st := o.obj("streamSettings")
	fingerprint := func(t *object) string {
		name := t.str("fingerprint")
		mapped, ok := fingerprints[name]
		if !ok {
			t.fail("fingerprint", fmt.Sprintf("fingerprint %q has no known counterpart in the core", name))
		}
		return mapped
	}
	switch sec := st.str("security"); sec {
	case "reality":
		r := st.obj("realitySettings")
		p["tls"] = true
		p["servername"] = r.str("serverName")
		fp := fingerprint(r)
		p["client-fingerprint"] = fp
		opts := map[string]any{"public-key": r.str("password")}
		if sid := r.str("shortId"); sid != "" {
			opts["short-id"] = sid
		}
		if carriesMLKEM[fp] {
			opts["support-x25519mlkem768"] = true
		}
		p["reality-opts"] = opts
	case "tls":
		t := st.obj("tlsSettings")
		p["tls"] = true
		p["servername"] = t.str("serverName")
		p["client-fingerprint"] = fingerprint(t)
		if alpn := t.strs("alpn"); len(alpn) > 0 {
			p["alpn"] = alpn
		}
		if names := t.str("verifyPeerCertByName"); names != "" {
			if strings.Contains(names, ",") {
				t.fail("verifyPeerCertByName", "the core checks the certificate against one name, this profile lists several")
			}
			p["name-cert-verify"] = names
		}
	default:
		st.fail("security", fmt.Sprintf("security %q is not supported", sec))
	}

	switch network := st.str("network"); network {
	case "xhttp":
		p["network"] = "xhttp"
		p["xhttp-opts"] = xhttpOpts(st.obj("xhttpSettings"))
	case "grpc":
		p["network"] = "grpc"
		p["grpc-opts"] = map[string]any{"grpc-service-name": st.obj("grpcSettings").str("serviceName")}
	case "raw", "tcp":
		p["network"] = "tcp"
	default:
		st.fail("network", fmt.Sprintf("transport %q is not supported", network))
	}

	if o.has("mux") {
		m := o.obj("mux")
		if m.boolean("enabled") {
			m.fail("enabled", "Mux.Cool is not in the compat core")
		}
		// The other keys of a mux that is off change nothing.
		for key := range m.fields {
			m.read[key] = true
		}
	}
	return p
}

var xhttpExtra = map[string]string{
	"xPaddingBytes":        "x-padding-bytes",
	"xPaddingObfsMode":     "x-padding-obfs-mode",
	"xPaddingKey":          "x-padding-key",
	"xPaddingHeader":       "x-padding-header",
	"xPaddingPlacement":    "x-padding-placement",
	"xPaddingMethod":       "x-padding-method",
	"uplinkHTTPMethod":     "uplink-http-method",
	"uplinkDataPlacement":  "uplink-data-placement",
	"uplinkDataKey":        "uplink-data-key",
	"scMaxEachPostBytes":   "sc-max-each-post-bytes",
	"scMinPostsIntervalMs": "sc-min-posts-interval-ms",
	"noGRPCHeader":         "no-grpc-header",
}

var xmuxKeys = map[string]string{
	"maxConcurrency":   "max-concurrency",
	"maxConnections":   "max-connections",
	"cMaxReuseTimes":   "c-max-reuse-times",
	"hMaxRequestTimes": "h-max-request-times",
	"hMaxReusableSecs": "h-max-reusable-secs",
}

func xhttpOpts(x *object) map[string]any {
	opts := map[string]any{}
	path := x.str("path")
	if path == "" {
		path = "/"
	}
	opts["path"] = path
	host, mode := x.str("host"), x.str("mode")

	// Without a pool mihomo opens a connection per session, where Xray applies
	// its xmux defaults; a CDN in front then cuts a long upload. So a profile
	// with no xmux gets Xray's defaults spelled out.
	reuse := map[string]any{
		"max-concurrency":     "16-32",
		"max-connections":     "0",
		"c-max-reuse-times":   "0",
		"h-max-request-times": "600-900",
		"h-max-reusable-secs": "1800-3000",
		"h-keep-alive-period": 0,
	}
	if x.has("extra") {
		e := x.obj("extra")
		// Throne repeats host and mode inside extra.
		same := func(key, top string) string {
			if !e.has(key) {
				return top
			}
			v := e.str(key)
			if top != "" && v != top {
				e.fail(key, fmt.Sprintf("extra.%s %q contradicts %q", key, v, top))
			}
			return v
		}
		host, mode = same("host", host), same("mode", mode)
		for from, to := range xhttpExtra {
			if e.has(from) {
				opts[to] = e.scalar(from)
			}
		}
		if e.has("xmux") {
			m := e.obj("xmux")
			// A partly set xmux turns the defaults of its other keys off in Xray.
			reuse = map[string]any{}
			for from, to := range xmuxKeys {
				if m.has(from) {
					reuse[to] = fmt.Sprint(m.scalar(from))
				}
			}
			if m.has("hKeepAlivePeriod") {
				reuse["h-keep-alive-period"] = m.num("hKeepAlivePeriod")
			}
		}
	}
	if host != "" {
		opts["host"] = host
	}
	if mode != "" {
		opts["mode"] = mode
	}
	opts["reuse-settings"] = reuse
	return opts
}

func singTLS(o *object, p map[string]any) {
	t := o.obj("tls")
	if !t.boolean("enabled") {
		t.fail("enabled", "the profile has TLS turned off")
	}
	p["sni"] = t.str("server_name")
	if alpn := t.strs("alpn"); len(alpn) > 0 {
		p["alpn"] = alpn
	}
}

func singTuic(o *object) map[string]any {
	p := map[string]any{
		"type":     "tuic",
		"server":   o.str("server"),
		"port":     o.num("server_port"),
		"uuid":     o.str("uuid"),
		"password": o.str("password"),
	}
	if cc := o.str("congestion_control"); cc != "" {
		p["congestion-controller"] = cc
	}
	if mode := o.str("udp_relay_mode"); mode != "" {
		p["udp-relay-mode"] = mode
	}
	singTLS(o, p)
	return p
}

func singHysteria2(o *object) map[string]any {
	p := map[string]any{
		"type":     "hysteria2",
		"server":   o.str("server"),
		"port":     o.num("server_port"),
		"password": o.str("password"),
	}
	singTLS(o, p)
	return p
}

// object reads a JSON object and remembers which keys were read, so that
// finish can refuse the ones nobody asked for.
type object struct {
	path     string
	fields   map[string]any
	read     map[string]bool
	children []*object
	errs     *[]string
}

func newObject(path string, fields map[string]any) *object {
	return &object{path: path, fields: fields, read: map[string]bool{}, errs: &[]string{}}
}

func (o *object) at(key string) string {
	if o.path == "" {
		return key
	}
	return o.path + "." + key
}

func (o *object) fail(key, message string) {
	*o.errs = append(*o.errs, o.at(key)+": "+message)
}

func (o *object) has(key string) bool {
	_, ok := o.fields[key]
	return ok
}

func (o *object) get(key string) (any, bool) {
	o.read[key] = true
	v, ok := o.fields[key]
	return v, ok && v != nil
}

func (o *object) str(key string) string {
	v, ok := o.get(key)
	if !ok {
		return ""
	}
	s, isString := v.(string)
	if !isString {
		o.fail(key, "expected a string")
	}
	return s
}

func (o *object) num(key string) int {
	v, ok := o.get(key)
	if !ok {
		return 0
	}
	f, isNumber := v.(float64)
	if !isNumber || f != float64(int(f)) {
		o.fail(key, "expected a whole number")
	}
	return int(f)
}

func (o *object) boolean(key string) bool {
	v, ok := o.get(key)
	if !ok {
		return false
	}
	b, isBool := v.(bool)
	if !isBool {
		o.fail(key, "expected true or false")
	}
	return b
}

// scalar keeps a string, number or boolean as the profile wrote it.
func (o *object) scalar(key string) any {
	v, _ := o.get(key)
	switch t := v.(type) {
	case string, bool:
		return t
	case float64:
		if t == float64(int(t)) {
			return int(t)
		}
		return t
	}
	o.fail(key, "expected a plain value")
	return nil
}

func (o *object) strs(key string) []string {
	v, ok := o.get(key)
	if !ok {
		return nil
	}
	list, isList := v.([]any)
	if !isList {
		o.fail(key, "expected a list")
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, isString := item.(string)
		if !isString {
			o.fail(key, "expected a list of strings")
			return nil
		}
		out = append(out, s)
	}
	return out
}

func (o *object) obj(key string) *object {
	v, ok := o.get(key)
	fields, isObject := v.(map[string]any)
	if !ok || !isObject {
		o.fail(key, "is missing")
		fields = map[string]any{}
	}
	child := &object{path: o.at(key), fields: fields, read: map[string]bool{}, errs: o.errs}
	o.children = append(o.children, child)
	return child
}

func (o *object) unread(into *[]string) {
	for key := range o.fields {
		if !o.read[key] {
			*into = append(*into, o.at(key))
		}
	}
	for _, child := range o.children {
		child.unread(into)
	}
}

func (o *object) finish() error {
	var unread []string
	o.unread(&unread)
	sort.Strings(unread)
	problems := append([]string{}, *o.errs...)
	if len(unread) > 0 {
		problems = append(problems, "unsupported fields: "+strings.Join(unread, ", "))
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

// allowed is every key a server may carry: exactly what this package writes.
// The privileged side checks a server against it before the core, which runs
// with the rights to change routes, is given anything.
var allowed = map[string]any{
	"type": nil, "server": nil, "port": nil, "uuid": nil, "password": nil,
	"flow": nil, "encryption": nil, "udp": nil, "packet-encoding": nil,
	"tls": nil, "servername": nil, "sni": nil, "alpn": nil,
	"client-fingerprint": nil, "name-cert-verify": nil, "network": nil,
	"congestion-controller": nil, "udp-relay-mode": nil,
	"cipher": nil, "alterId": nil, "skip-cert-verify": nil, "obfs": nil, "obfs-password": nil,
	"ws-opts": map[string]any{
		"path": nil, "headers": map[string]any{"Host": nil},
		"max-early-data": nil, "early-data-header-name": nil,
	},
	"reality-opts": map[string]any{"public-key": nil, "short-id": nil, "support-x25519mlkem768": nil},
	"grpc-opts":    map[string]any{"grpc-service-name": nil},
	"xhttp-opts": map[string]any{
		"path": nil, "host": nil, "mode": nil,
		"x-padding-bytes": nil, "x-padding-obfs-mode": nil, "x-padding-key": nil,
		"x-padding-header": nil, "x-padding-placement": nil, "x-padding-method": nil,
		"uplink-http-method": nil, "uplink-data-placement": nil, "uplink-data-key": nil,
		"sc-max-each-post-bytes": nil, "sc-min-posts-interval-ms": nil, "no-grpc-header": nil,
		"reuse-settings": map[string]any{
			"max-concurrency": nil, "max-connections": nil, "c-max-reuse-times": nil,
			"h-max-request-times": nil, "h-max-reusable-secs": nil, "h-keep-alive-period": nil,
		},
	},
}

var serverTypes = map[string]bool{"vless": true, "tuic": true, "hysteria2": true, "trojan": true, "ss": true, "vmess": true}

// Check refuses a server that is not in the shape this package produces.
func Check(s Server) error {
	if s.Name == "" {
		return errors.New("the server has no name")
	}
	if t, _ := s.Proxy["type"].(string); !serverTypes[t] {
		return fmt.Errorf("server type %q is not allowed", s.Proxy["type"])
	}
	if host, _ := s.Proxy["server"].(string); host == "" {
		return errors.New("the server has no address")
	}
	return checkKeys("", s.Proxy, allowed)
}

func checkKeys(path string, got, want map[string]any) error {
	for key, value := range got {
		nested, known := want[key]
		if !known {
			return fmt.Errorf("server field %q is not allowed", path+key)
		}
		switch v := value.(type) {
		case map[string]any:
			sub, isObject := nested.(map[string]any)
			if !isObject {
				return fmt.Errorf("server field %q must be a plain value", path+key)
			}
			if err := checkKeys(path+key+".", v, sub); err != nil {
				return err
			}
		case []any:
			for _, item := range v {
				if _, isString := item.(string); !isString {
					return fmt.Errorf("server field %q must be a list of strings", path+key)
				}
			}
		case []string, string, bool, int, float64:
			if nested != nil {
				return fmt.Errorf("server field %q must be an object", path+key)
			}
		default:
			return fmt.Errorf("server field %q has an unexpected value", path+key)
		}
	}
	return nil
}
