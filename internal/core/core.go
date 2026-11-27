// Package core runs mihomo inside this process.
package core

import (
	"fmt"
	"os"
	"sync"

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
