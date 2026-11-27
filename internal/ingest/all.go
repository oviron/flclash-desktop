package ingest

import (
	"strings"

	"github.com/oviron/flclash-desktop/internal/say"
	"gopkg.in/yaml.v3"
)

// Refusal is one entry of a document that could not be read.
type Refusal struct {
	Entry  int // 1-based position among the entries
	Reason string
}

// ParseAll reads every server in a document: links or Throne profiles one per
// line, the base64 body a subscription serves, or a Clash config with its
// `proxies` list. A refused entry does not stop the rest. Entries counts what
// the document held, read or not.
func ParseAll(text string) (servers []Server, refused []Refusal, entries int) {
	if lines, ok := IsSubscriptionBody(text); ok {
		text = lines
	}
	if proxies, ok := clashProxies(text); ok {
		for i, proxy := range proxies {
			server, err := fromClash(proxy)
			if err != nil {
				refused = append(refused, Refusal{Entry: i + 1, Reason: say.Text(err)})
				continue
			}
			servers = append(servers, server)
		}
		return servers, refused, len(proxies)
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entries++
		server, err := Parse(line)
		if err != nil {
			refused = append(refused, Refusal{Entry: entries, Reason: say.Text(err)})
			continue
		}
		servers = append(servers, server)
	}
	return servers, refused, entries
}

// clashProxies returns the `proxies` of a Clash config, when text is one.
func clashProxies(text string) ([]map[string]any, bool) {
	// A cheap look first: a list of links is not worth a YAML parser's guess.
	found := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "proxies:") {
			found = true
			break
		}
	}
	if !found {
		return nil, false
	}
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, false
	}
	return doc.Proxies, true
}

// fromClash takes a proxy of a Clash config as it is: it is already in the
// shape the core reads. It still has to pass Check, so a field this app does
// not know refuses the server rather than riding along unseen.
func fromClash(proxy map[string]any) (Server, error) {
	name, _ := proxy["name"].(string)
	if strings.TrimSpace(name) == "" {
		return Server{}, say.Errorf("the proxy has no name")
	}
	fields := map[string]any{}
	for key, value := range proxy {
		if key != "name" {
			fields[key] = value
		}
	}
	server := Server{Name: strings.TrimSpace(name), Proxy: fields}
	if err := Check(server); err != nil {
		return Server{}, say.Errorf("%s: %w", name, err)
	}
	return server, nil
}
