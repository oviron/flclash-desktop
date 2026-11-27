package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// NewSystemProxy returns the system proxy of macOS, set per network service
// with networksetup. The helper runs it as root, so nobody is asked anything.
func NewSystemProxy() SystemProxy { return networkSetup{} }

type networkSetup struct{}

// The three proxies a service has; each is read with -get<name> and written
// with -set<name>.
var proxyKinds = []string{"webproxy", "securewebproxy", "socksfirewallproxy"}

type proxySetting struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server,omitempty"`
	Port    int    `json:"port,omitempty"`
}

// savedService is what a network service had before Set.
type savedService struct {
	Proxies map[string]proxySetting `json:"proxies"`
	Bypass  []string                `json:"bypass"`
}

func savedProxyPath() string { return filepath.Join(StateDir(), "system-proxy.json") }

func networksetup(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/networksetup", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("networksetup %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// services lists the network services that are enabled.
func services() ([]string, error) {
	out, err := networksetup("-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	return parseServices(out), nil
}

func parseServices(out string) []string {
	var names []string
	for i, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// The first line is a note; a star marks a disabled service.
		if i == 0 || line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		names = append(names, line)
	}
	return names
}

func parseProxy(out string) proxySetting {
	var p proxySetting
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "Enabled":
			p.Enabled = value == "Yes"
		case "Server":
			p.Server = value
		case "Port":
			p.Port, _ = strconv.Atoi(value)
		}
	}
	return p
}

func parseBypass(out string) []string {
	var domains []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// With none set, networksetup answers with a sentence.
		if line == "" || strings.Contains(line, " ") {
			continue
		}
		domains = append(domains, line)
	}
	return domains
}

func (networkSetup) Set(port int) error {
	names, err := services()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("there is no enabled network service to set the proxy on")
	}
	// What was there is saved once and before anything changes: a second Set
	// must not save our own port as the thing to go back to.
	saved := map[string]savedService{}
	if raw, err := os.ReadFile(savedProxyPath()); err == nil {
		_ = json.Unmarshal(raw, &saved)
	}
	if len(saved) == 0 {
		for _, name := range names {
			entry := savedService{Proxies: map[string]proxySetting{}}
			for _, kind := range proxyKinds {
				out, err := networksetup("-get"+kind, name)
				if err != nil {
					return err
				}
				entry.Proxies[kind] = parseProxy(out)
			}
			out, err := networksetup("-getproxybypassdomains", name)
			if err != nil {
				return err
			}
			entry.Bypass = parseBypass(out)
			saved[name] = entry
		}
		raw, _ := json.Marshal(saved)
		if err := os.WriteFile(savedProxyPath(), raw, 0o600); err != nil {
			return err
		}
	}
	for _, name := range names {
		for _, kind := range proxyKinds {
			if _, err := networksetup("-set"+kind, name, "127.0.0.1", strconv.Itoa(port), "off"); err != nil {
				return err
			}
		}
		bypass := append([]string{}, saved[name].Bypass...)
		for _, domain := range proxyBypass {
			if !contains(bypass, domain) {
				bypass = append(bypass, domain)
			}
		}
		if _, err := networksetup(append([]string{"-setproxybypassdomains", name}, bypass...)...); err != nil {
			return err
		}
	}
	return nil
}

func (networkSetup) Clear() error {
	raw, err := os.ReadFile(savedProxyPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	saved := map[string]savedService{}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	var failed []string
	for name, entry := range saved {
		for _, kind := range proxyKinds {
			was := entry.Proxies[kind]
			// Writing the address turns the proxy on, so one that was off is
			// written first and turned off after: its fields go back as well.
			if was.Server != "" {
				if _, err := networksetup("-set"+kind, name, was.Server, strconv.Itoa(was.Port)); err != nil {
					failed = append(failed, err.Error())
				}
			}
			if !was.Enabled || was.Server == "" {
				if _, err := networksetup("-set"+kind+"state", name, "off"); err != nil {
					failed = append(failed, err.Error())
				}
			}
		}
		bypass := entry.Bypass
		if len(bypass) == 0 {
			bypass = []string{"Empty"}
		}
		if _, err := networksetup(append([]string{"-setproxybypassdomains", name}, bypass...)...); err != nil {
			failed = append(failed, err.Error())
		}
	}
	// A service that is gone cannot be restored, and never will be: the file
	// goes either way, so that the next Set saves the state of that moment.
	_ = os.Remove(savedProxyPath())
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "; "))
	}
	return nil
}

func contains(list []string, item string) bool {
	for _, x := range list {
		if x == item {
			return true
		}
	}
	return false
}
