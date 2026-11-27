// Package core runs mihomo inside this process.
package core

import (
	"context"
	"errors"
	"fmt"
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

// Delay opens a new connection through a server, asks for the URL twice and
// says how long the second answer took, in milliseconds: the time of one
// round trip over a connection that is already up. The first request pays for
// the handshakes, several round trips of them, and people read the number as
// a ping. It needs no running config, and it leaves a running one alone.
func Delay(proxy map[string]any, url string, timeout time.Duration) (int, error) {
	// Applying a config sets this from the config, so it is set on every call.
	adapter.UnifiedDelay.Store(true)
	p, err := adapter.ParseProxy(proxy)
	if err != nil {
		return 0, fmt.Errorf("the core refused the server: %w", err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ms, err := p.URLTest(ctx, url, nil)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return 0, fmt.Errorf("no answer in %s", timeout)
		}
		return 0, err
	}
	return int(ms), nil
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
