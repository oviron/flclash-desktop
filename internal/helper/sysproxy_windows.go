package helper

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/oviron/flclash-desktop/internal/say"
)

// NewWindowsProxy returns the system proxy of Windows: the WinINet setting of
// the user the helper serves. It lives in the user's profile and only a
// program of the user's session can change it so that others hear, so the
// helper has the core do it as the user.
func NewWindowsProxy(sid, corePath string) SystemProxy {
	return &winINet{sid: sid, core: corePath}
}

type winINet struct {
	sid  string
	core string
}

func savedProxyPath() string { return filepath.Join(StateDir(), "system-proxy.json") }

func (w *winINet) Set(port int) error {
	// What was there before is kept once: a second Set must not save our own
	// setting as the one to go back to.
	if _, err := os.Stat(savedProxyPath()); errors.Is(err, os.ErrNotExist) {
		was, err := runAsUser(w.sid, nil, w.core, "-sysproxy", "get")
		if err != nil {
			return say.Errorf("the system proxy could not be read: %w", err)
		}
		if !json.Valid(was) {
			return say.New("the system proxy could not be read")
		}
		if err := os.WriteFile(savedProxyPath(), was, 0o600); err != nil {
			return err
		}
	}
	if _, err := runAsUser(w.sid, nil, w.core, "-sysproxy", "set", strconv.Itoa(port)); err != nil {
		return err
	}
	return nil
}

func (w *winINet) Clear() error {
	was, err := os.ReadFile(savedProxyPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := runAsUser(w.sid, was, w.core, "-sysproxy", "restore"); err != nil {
		// The file stays: the next Clear tries again, when the user is back.
		return err
	}
	return os.Remove(savedProxyPath())
}
