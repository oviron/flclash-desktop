// Package core runs mihomo inside this process.
package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/adapter"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub/executor"
	"github.com/metacubex/mihomo/tunnel/statistic"

	"github.com/oviron/flclash-desktop/internal/helper"
)

var mu sync.Mutex

// Apply starts the core with this config, or moves a running core to it.
// A config the core refuses leaves the running one in place.
func Apply(homeDir string, config []byte) error {
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return err
	}
	C.SetHomeDir(homeDir)
	parsed, err := executor.ParseWithBytes(config)
	if err != nil {
		return fmt.Errorf("the core refused the config: %w", err)
	}
	executor.ApplyConfig(parsed, true)
	return nil
}

// Stop closes the listeners and connections of the core.
func Stop() {
	mu.Lock()
	defer mu.Unlock()
	executor.Shutdown()
}

// delaySamples is how many round trips a measurement times.
const delaySamples = 3

// Delay measures a server: it opens a connection to the URL through it, makes
// one request to pay for the handshakes, then times a few more over the same
// connection and gives the middle one, in milliseconds. That is one round
// trip to a site through the server, which is what people mean by its ping.
//
// The number is always a measurement. A server that answered the first
// request and not the ones that are timed has no number: it failed.
// It needs no running config, and it leaves a running one alone.
func Delay(proxy map[string]any, rawURL string, timeout time.Duration) (int, error) {
	link, err := through(proxy, rawURL, timeout)
	if err != nil {
		return 0, err
	}
	defer link.close()
	request := func() (time.Duration, error) {
		start := time.Now()
		resp, err := link.do(http.MethodHead)
		if err != nil {
			return 0, err
		}
		_ = resp.Body.Close()
		return time.Since(start), nil
	}
	if _, err := request(); err != nil {
		return 0, link.fail(err)
	}
	samples := make([]time.Duration, 0, delaySamples)
	for range delaySamples {
		took, err := request()
		if err != nil {
			return 0, link.fail(err)
		}
		samples = append(samples, took)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return max(int(samples[len(samples)/2]/time.Millisecond), 1), nil
}

// Exit asks a service on the far side of a server where the request came
// from: the address sites see and its country. The request goes through the
// server whatever the routing policy says, so the answer is about the server.
func Exit(proxy map[string]any, rawURL string, timeout time.Duration) (helper.Exit, error) {
	link, err := through(proxy, rawURL, timeout)
	if err != nil {
		return helper.Exit{}, err
	}
	defer link.close()
	resp, err := link.do(http.MethodGet)
	if err != nil {
		return helper.Exit{}, link.fail(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil {
		return helper.Exit{}, link.fail(err)
	}
	exit, ok := parseTrace(string(body))
	if resp.StatusCode != http.StatusOK || !ok {
		return helper.Exit{}, fmt.Errorf("the service did not say the address (status %d)", resp.StatusCode)
	}
	return exit, nil
}

// parseTrace reads the key=value lines of Cloudflare's trace page.
func parseTrace(body string) (helper.Exit, bool) {
	var exit helper.Exit
	for _, line := range strings.Split(body, "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch key {
		case "ip":
			if addr, err := netip.ParseAddr(value); err == nil {
				exit.IP = addr.String()
			}
		case "loc":
			// A two-letter country code; XX and T1 mean it is not known.
			if len(value) == 2 && value != "XX" && value != "T1" {
				exit.Country = strings.ToUpper(value)
			}
		}
	}
	return exit, exit.IP != ""
}

// link is one connection to the host of a URL through a server, and requests
// over it.
type link struct {
	client http.Client
	url    string
	ctx    context.Context
	close  func()
	wait   time.Duration
}

func (l *link) do(method string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(l.ctx, method, l.url, nil)
	if err != nil {
		return nil, err
	}
	return l.client.Do(req)
}

// fail says "no answer" for an error that is the deadline passing.
func (l *link) fail(err error) error {
	if l.ctx.Err() != nil {
		return fmt.Errorf("no answer in %s", l.wait)
	}
	return err
}

func through(proxy map[string]any, rawURL string, timeout time.Duration) (*link, error) {
	p, err := adapter.ParseProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("the core refused the server: %w", err)
	}
	target, err := url.Parse(rawURL)
	if err != nil {
		_ = p.Close()
		return nil, err
	}
	port := target.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[target.Scheme]
	}
	var destination C.Metadata
	if err := destination.SetRemoteAddress(net.JoinHostPort(target.Hostname(), port)); err != nil {
		_ = p.Close()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	l := &link{url: rawURL, ctx: ctx, wait: timeout}
	conn, err := p.DialContext(ctx, &destination)
	if err != nil {
		err = l.fail(err)
		cancel()
		_ = p.Close()
		return nil, err
	}
	// One connection for every request: a second dial would measure another
	// handshake, not a round trip.
	dialed := false
	l.client = http.Client{
		Transport: &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				if dialed {
					return nil, errors.New("the connection to the server closed between requests")
				}
				dialed = true
				return conn, nil
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	l.close = func() {
		l.client.CloseIdleConnections()
		_ = conn.Close()
		cancel()
		_ = p.Close()
	}
	return l, nil
}

// Stats reads the traffic counters and the open connections.
func Stats() helper.Stats {
	manager := statistic.DefaultManager
	stats := helper.Stats{Connections: []helper.Connection{}}
	stats.Up, stats.Down = manager.Now()
	stats.UpTotal, stats.DownTotal = manager.Total()
	manager.Range(func(tracker statistic.Tracker) bool {
		info := tracker.Info()
		meta := info.Metadata
		if meta == nil {
			return true
		}
		host := meta.Host
		if host == "" {
			host = meta.DstIP.String()
		}
		via := ""
		if len(info.Chain) > 0 {
			// The chain runs from the outbound that carries the connection up
			// to the group the rule named.
			via = info.Chain[0]
		}
		rule := info.Rule
		if info.RulePayload != "" {
			rule += "," + info.RulePayload
		}
		stats.Connections = append(stats.Connections, helper.Connection{
			ID:       info.UUID.String(),
			Host:     host,
			Port:     int(meta.DstPort),
			Network:  meta.NetWork.String(),
			Via:      via,
			Rule:     rule,
			Upload:   info.UploadTotal.Load(),
			Download: info.DownloadTotal.Load(),
			Start:    info.Start.Unix(),
		})
		return true
	})
	stats.Open = len(stats.Connections)
	sort.Slice(stats.Connections, func(i, j int) bool {
		a, b := stats.Connections[i], stats.Connections[j]
		if a.Start != b.Start {
			return a.Start > b.Start
		}
		return a.ID < b.ID
	})
	if len(stats.Connections) > helper.MaxConnections {
		stats.Connections = stats.Connections[:helper.MaxConnections]
	}
	return stats
}
