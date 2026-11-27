package helper

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/oviron/flclash-desktop/internal/say"
	"github.com/oviron/flclash-desktop/internal/winproxy"
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

// userSystemProxy is the system proxy as the app itself sets it: the app runs
// as the user, so it changes the setting directly.
func userSystemProxy(home string) SystemProxy {
	return userINet{saved: filepath.Join(home, "system-proxy.json")}
}

// userINet keeps what it replaced in the file saved names.
type userINet struct{ saved string }

func (u userINet) Set(port int) error {
	// What was there before is kept once, as in winINet.Set.
	if _, err := os.Stat(u.saved); errors.Is(err, os.ErrNotExist) {
		was, err := winproxy.Read()
		if err != nil {
			return say.Errorf("the system proxy could not be read: %w", err)
		}
		raw, err := json.Marshal(was)
		if err != nil {
			return err
		}
		if err := os.WriteFile(u.saved, raw, 0o600); err != nil {
			return err
		}
	}
	return winproxy.Point(port)
}

func (u userINet) Clear() error {
	raw, err := os.ReadFile(u.saved)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var was winproxy.Setting
	if err := json.Unmarshal(raw, &was); err != nil {
		// What cannot be read cannot be put back; it is not kept either.
		_ = os.Remove(u.saved)
		return err
	}
	if err := winproxy.Write(was); err != nil {
		return err
	}
	return os.Remove(u.saved)
}
