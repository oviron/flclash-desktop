// Package core runs mihomo inside this process.
package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/metacubex/mihomo/adapter"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub/executor"
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

// Delay opens a new connection through a server, asks for the URL and says how
// long the answer took, in milliseconds. It needs no running config, and it
// leaves a running one alone.
func Delay(proxy map[string]any, url string, timeout time.Duration) (int, error) {
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
