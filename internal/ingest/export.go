package ingest

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/oviron/flclash-desktop/internal/say"
)

// Link writes a server as a share link, the kind Parse reads. A server with
// a setting no link can carry is refused: a link that silently lost it would
// make another server on the far side.
func Link(s Server) (string, error) {
	w := &writer{proxy: s.Proxy, used: map[string]bool{"type": true, "server": true, "port": true}, query: url.Values{}}
	host, port := w.text("server"), w.number("port")
	if host == "" || port == 0 {
		return "", say.New("the server has no address")
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	var link string
	switch kind := w.text("type"); kind {
	case "vless":
		w.used["udp"] = true
		switch encoding := w.take("packet-encoding"); encoding {
		case "xudp":
		case "packetaddr":
			w.query.Set("packetEncoding", "packetaddr")
		case "":
			w.query.Set("packetEncoding", "none")
		}
		w.param("flow", "flow")
		w.param("encryption", "encryption")
		if opts := w.object("reality-opts"); opts != nil {
			w.used["tls"] = true
			w.query.Set("security", "reality")
			w.param("servername", "sni")
			w.fingerprint()
			w.query.Set("pbk", opts.take("public-key"))
			if sid := opts.take("short-id"); sid != "" {
				w.query.Set("sid", sid)
			}
			opts.used["support-x25519mlkem768"] = true
			w.leftover(opts, "reality-opts")
		} else if w.flag("tls") {
			w.query.Set("security", "tls")
			w.tls("servername")
		}
		w.transport()
		link = w.url("vless", url.User(w.take("uuid")), address, s.Name)
	case "trojan":
		w.used["udp"] = true
		w.query.Set("security", "tls")
		w.tls("sni")
		w.transport()
		link = w.url("trojan", url.User(w.take("password")), address, s.Name)
	case "hysteria2":
		w.tls("sni")
		if obfs := w.take("obfs"); obfs != "" {
			w.query.Set("obfs", obfs)
			w.query.Set("obfs-password", w.take("obfs-password"))
		}
		user := url.User(w.text("password"))
		if first, rest, split := strings.Cut(w.take("password"), ":"); split {
			user = url.UserPassword(first, rest)
		}
		link = w.url("hysteria2", user, address, s.Name)
	case "tuic":
		w.param("congestion-controller", "congestion_control")
		w.param("udp-relay-mode", "udp_relay_mode")
		w.tls("sni")
		link = w.url("tuic", url.UserPassword(w.take("uuid"), w.take("password")), address, s.Name)
	case "ss":
		w.used["udp"] = true
		login := base64.RawURLEncoding.EncodeToString([]byte(w.take("cipher") + ":" + w.take("password")))
		link = "ss://" + login + "@" + address + "#" + url.PathEscape(s.Name)
	case "vmess":
		link = w.vmess(host, port, s.Name)
	default:
		return "", say.Errorf("servers of the kind %q have no link form", kind)
	}
	w.leftover(w, "")
	if len(w.errs) > 0 {
		sort.Strings(w.errs)
		return "", say.Errorf("a link cannot carry this server's %s", strings.Join(w.errs, ", "))
	}
	return link, nil
}

// writer reads a proxy and remembers what it read, as object does the other
// way: a key nobody read is a setting the link would lose.
type writer struct {
	proxy map[string]any
	used  map[string]bool
	query url.Values
	errs  []string
}

func (w *writer) text(key string) string {
	switch v := w.proxy[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	}
	return ""
}

func (w *writer) take(key string) string {
	w.used[key] = true
	return w.text(key)
}

func (w *writer) number(key string) int {
	w.used[key] = true
	switch v := w.proxy[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}

func (w *writer) flag(key string) bool {
	w.used[key] = true
	on, _ := w.proxy[key].(bool)
	return on
}

func (w *writer) list(key string) []string {
	w.used[key] = true
	var out []string
	switch v := w.proxy[key].(type) {
	case []string:
		out = v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func (w *writer) object(key string) *writer {
	w.used[key] = true
	fields, ok := w.proxy[key].(map[string]any)
	if !ok {
		return nil
	}
	return &writer{proxy: fields, used: map[string]bool{}, query: w.query}
}

// param copies a setting into the query under the name links use for it.
func (w *writer) param(key, name string) {
	if value := w.take(key); value != "" {
		w.query.Set(name, value)
	}
}

func (w *writer) leftover(of *writer, path string) {
	for key := range of.proxy {
		if !of.used[key] {
			if path != "" {
				key = path + "." + key
			}
			w.errs = append(w.errs, key)
		}
	}
}

// fingerprint writes the uTLS name under the name the links use for it.
func (w *writer) fingerprint() {
	name := w.take("client-fingerprint")
	for linkName, coreName := range fingerprints {
		if coreName == name {
			name = linkName
		}
	}
	if name != "" {
		w.query.Set("fp", name)
	}
}

func (w *writer) tls(sniKey string) {
	w.param(sniKey, "sni")
	if alpn := w.list("alpn"); len(alpn) > 0 {
		w.query.Set("alpn", strings.Join(alpn, ","))
	}
	w.fingerprint()
	if w.flag("skip-cert-verify") {
		w.query.Set("allowInsecure", "1")
	}
}

func (w *writer) transport() {
	switch network := w.take("network"); network {
	case "", "tcp":
		w.query.Set("type", "tcp")
	case "ws":
		w.query.Set("type", "ws")
		if opts := w.object("ws-opts"); opts != nil {
			path := opts.take("path")
			if early := opts.number("max-early-data"); early > 0 {
				path += "?ed=" + strconv.Itoa(early)
			}
			opts.used["early-data-header-name"] = true
			w.query.Set("path", path)
			if headers := opts.object("headers"); headers != nil {
				if host := headers.take("Host"); host != "" {
					w.query.Set("host", host)
				}
				w.leftover(headers, "ws-opts.headers")
			}
			w.leftover(opts, "ws-opts")
		}
	case "grpc":
		w.query.Set("type", "grpc")
		if opts := w.object("grpc-opts"); opts != nil {
			w.query.Set("serviceName", opts.take("grpc-service-name"))
			w.leftover(opts, "grpc-opts")
		}
	case "xhttp":
		w.query.Set("type", "xhttp")
		opts := w.object("xhttp-opts")
		if opts == nil {
			return
		}
		for _, key := range []string{"path", "host", "mode"} {
			if value := opts.take(key); value != "" {
				w.query.Set(key, value)
			}
		}
		extra := map[string]any{}
		for linkKey, coreKey := range xhttpExtra {
			if value, has := opts.proxy[coreKey]; has {
				opts.used[coreKey] = true
				extra[linkKey] = value
			}
		}
		if reuse := opts.object("reuse-settings"); reuse != nil {
			xmux := map[string]any{}
			for linkKey, coreKey := range xmuxKeys {
				if value, has := reuse.proxy[coreKey]; has {
					reuse.used[coreKey] = true
					xmux[linkKey] = value
				}
			}
			if _, has := reuse.proxy["h-keep-alive-period"]; has {
				xmux["hKeepAlivePeriod"] = reuse.number("h-keep-alive-period")
			}
			w.leftover(reuse, "xhttp-opts.reuse-settings")
			extra["xmux"] = xmux
		}
		if len(extra) > 0 {
			raw, _ := json.Marshal(extra)
			w.query.Set("extra", string(raw))
		}
		w.leftover(opts, "xhttp-opts")
	default:
		w.errs = append(w.errs, "transport "+network)
	}
}

func (w *writer) url(scheme string, user *url.Userinfo, address, name string) string {
	u := url.URL{Scheme: scheme, User: user, Host: address, RawQuery: w.query.Encode(), Fragment: name}
	return u.String()
}

// vmess writes the base64 JSON form V2RayN set.
func (w *writer) vmess(host string, port int, name string) string {
	w.used["udp"] = true
	fields := map[string]any{
		"v": "2", "ps": name, "add": host, "port": strconv.Itoa(port),
		"id": w.take("uuid"), "aid": strconv.Itoa(w.number("alterId")), "scy": w.take("cipher"), "type": "none",
	}
	switch network := w.take("network"); network {
	case "", "tcp":
		fields["net"] = "tcp"
	case "ws":
		fields["net"] = "ws"
		if opts := w.object("ws-opts"); opts != nil {
			fields["path"] = opts.take("path")
			if headers := opts.object("headers"); headers != nil {
				fields["host"] = headers.take("Host")
				w.leftover(headers, "ws-opts.headers")
			}
			w.leftover(opts, "ws-opts")
		}
	case "grpc":
		fields["net"] = "grpc"
		if opts := w.object("grpc-opts"); opts != nil {
			fields["path"] = opts.take("grpc-service-name")
			w.leftover(opts, "grpc-opts")
		}
	default:
		w.errs = append(w.errs, "transport "+network)
	}
	if w.flag("tls") {
		fields["tls"] = "tls"
		fields["sni"] = w.take("servername")
		if alpn := w.list("alpn"); len(alpn) > 0 {
			fields["alpn"] = strings.Join(alpn, ",")
		}
		if fp := w.take("client-fingerprint"); fp != "" {
			for linkName, coreName := range fingerprints {
				if coreName == fp {
					fp = linkName
				}
			}
			fields["fp"] = fp
		}
	}
	raw, _ := json.Marshal(fields)
	return "vmess://" + base64.StdEncoding.EncodeToString(raw)
}
