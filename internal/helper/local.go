package helper

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oviron/flclash-desktop/internal/say"
)

// The proxy modes need no privileges: the core runs as the user either way,
// and the system proxy is the user's own setting. So the app runs them itself,
// with the same supervisor, and the helper is needed only for the tunnel. A
// person who never turns the tunnel on never installs it.
//
// Only a process that stays runs the proxy modes: the app. Elsewhere, as in
// the command line, there is no local supervisor and every request goes to
// the helper, as before.

var errNotInstalled = notInstalled()

// IsNotInstalled says whether an error means there is no helper to ask.
func IsNotInstalled(err error) bool { return errors.Is(err, errNotInstalled) }

var local struct {
	mu  sync.Mutex
	s   *Supervisor
	log string
}

// RunLocal makes this process run the proxy modes itself. home is where the
// core keeps its files and what it replaced in the system; the core is the
// binary at corePath.
func RunLocal(corePath, home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(home, "core.log")
	// The log only needs the recent past.
	if info, err := os.Stat(logPath); err == nil && info.Size() > 1<<20 {
		_ = os.Remove(logPath)
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	s := &Supervisor{CorePath: corePath, HomeDir: home, Log: file, SystemProxy: userSystemProxy(home)}
	s.init()
	local.mu.Lock()
	local.s, local.log = s, logPath
	local.mu.Unlock()
	return nil
}

// LocalLog is the file the core the app runs writes to; empty when the app
// runs none.
func LocalLog() string {
	local.mu.Lock()
	defer local.mu.Unlock()
	return local.log
}

func localSupervisor() *Supervisor {
	local.mu.Lock()
	defer local.mu.Unlock()
	return local.s
}

// active: a core runs, or what its mode promised holds after it died.
func active(st Status) bool { return st.Running || st.Blocked }

// call sends a request where it belongs: the tunnel to the helper, the proxy
// modes to the app's own supervisor, the rest to whichever of the two has
// something running. One port is served once: turning one side on turns the
// other off first.
func call(req Request, timeout time.Duration) (Response, error) {
	l := localSupervisor()
	if l == nil {
		return remote(req, timeout)
	}
	req.Lang = say.Language()
	switch req.Op {
	case OpApply:
		if req.Mode == ModeTUN {
			if active(l.Status()) {
				l.Stop()
			}
			return remote(req, timeout)
		}
		resp, err := remote(Request{Op: OpStatus}, 10*time.Second)
		switch {
		case err == nil && active(resp.Status):
			if resp, err := remote(Request{Op: OpStop}, 30*time.Second); err != nil {
				return resp, err
			}
		case err != nil && !IsNotInstalled(err):
			// A helper that does not answer may hold a tunnel on this port.
			return resp, say.Errorf("the helper does not answer, so a tunnel may still run: %w", err)
		}
		return answer(l, req)
	case OpStop:
		resp, err := answer(l, req)
		if helper, herr := remote(req, timeout); herr == nil {
			resp = helper
		} else if !IsNotInstalled(herr) && err == nil {
			// The helper may still hold a tunnel; that it was not reached
			// is the news.
			err = herr
		}
		return resp, err
	}
	if active(l.Status()) {
		return answer(l, req)
	}
	resp, err := remote(req, timeout)
	if req.Op == OpStatus && err != nil {
		// Without a helper what is true is what the app runs: nothing.
		return answer(l, req)
	}
	return resp, err
}

func answer(s *Supervisor, req Request) (Response, error) {
	resp := s.Answer(req)
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

// HelperState is what the app knows of the helper, needed for the tunnel only.
type HelperState struct {
	// Installed is false when there is no helper to ask.
	Installed bool
	// Outdated: the helper is from another version of the app and has to be
	// installed again before it takes this app's requests.
	Outdated bool
	// Problem: the helper is there but did not answer.
	Problem error
}

// Probe asks the helper whether it is there and current.
func Probe() HelperState {
	resp, err := remote(Request{Op: OpStatus}, 10*time.Second)
	switch {
	case err == nil:
		return HelperState{Installed: true, Outdated: resp.Status.Version != Version}
	case IsNotInstalled(err):
		return HelperState{}
	}
	return HelperState{Installed: true, Problem: err}
}
