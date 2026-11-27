// Package core runs mihomo inside this process.
package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
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
	p, err := adapter.ParseProxy(proxy)
	if err != nil {
		return 0, fmt.Errorf("the core refused the server: %w", err)
	}
	defer p.Close()
	target, err := url.Parse(rawURL)
	if err != nil {
		return 0, err
	}
	port := target.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[target.Scheme]
	}
	var destination C.Metadata
	if err := destination.SetRemoteAddress(net.JoinHostPort(target.Hostname(), port)); err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fail := func(err error) (int, error) {
		if ctx.Err() != nil {
			return 0, fmt.Errorf("no answer in %s", timeout)
		}
		return 0, err
	}
	conn, err := p.DialContext(ctx, &destination)
	if err != nil {
		return fail(err)
	}
	defer conn.Close()
	// One connection for every request: a second dial would measure another
	// handshake, not a round trip.
	dialed := false
	client := http.Client{
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
	defer client.CloseIdleConnections()
	request := func() (time.Duration, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
		if err != nil {
			return 0, err
		}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		_ = resp.Body.Close()
		return time.Since(start), nil
	}
	if _, err := request(); err != nil {
		return fail(err)
	}
	samples := make([]time.Duration, 0, delaySamples)
	for range delaySamples {
		took, err := request()
		if err != nil {
			return fail(err)
		}
		samples = append(samples, took)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return max(int(samples[len(samples)/2]/time.Millisecond), 1), nil
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
