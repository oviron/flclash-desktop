package helper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// pfAnchor is where the rules live. The stock /etc/pf.conf evaluates every
// anchor under "com.apple", so the main ruleset is left as the system has it.
const pfAnchor = "com.apple/flclash"

// NewGuard returns the guard of this system: an anchor of the pf firewall.
func NewGuard() Guard { return pfGuard{} }

type pfGuard struct{}

func pfctl(stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/sbin/pfctl", args...)
	cmd.Stdin = strings.NewReader(stdin)
	// pfctl says what it did on stderr, and its warnings too.
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

func tokenPath() string { return filepath.Join(StateDir(), "pf-token") }

var pfToken = regexp.MustCompile(`Token : (\d+)`)

func (pfGuard) Engage(tun string, coreGID int, full bool) error {
	main, err := pfctl("", "-sr")
	if err != nil {
		return fmt.Errorf("the firewall does not answer: %v: %s", err, strings.TrimSpace(main))
	}
	if !strings.Contains(main, `anchor "com.apple/*"`) {
		return errors.New(`the firewall config of this Mac does not load "com.apple/*" anchors, so the rules would not apply`)
	}
	rules := pfRules(tun, coreGID, full, connectedNetworks())
	if out, err := pfctl(rules, "-a", pfAnchor, "-f", "-"); err != nil {
		return fmt.Errorf("the firewall refused the rules: %v: %s", err, strings.TrimSpace(out))
	}
	// The firewall may be off. Turning it on gives a token, and the token is
	// what turns it off again without taking it from someone else who needs it.
	if _, err := os.Stat(tokenPath()); err != nil {
		out, err := pfctl("", "-E")
		token := pfToken.FindStringSubmatch(out)
		if err != nil || token == nil {
			_, _ = pfctl("", "-a", pfAnchor, "-F", "all")
			return fmt.Errorf("the firewall did not turn on: %v: %s", err, strings.TrimSpace(out))
		}
		if err := os.WriteFile(tokenPath(), []byte(token[1]), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (pfGuard) Release() error {
	out, err := pfctl("", "-a", pfAnchor, "-F", "all")
	if token, readErr := os.ReadFile(tokenPath()); readErr == nil {
		// A token from before a restart of the system is of no use; that is fine.
		_, _ = pfctl("", "-X", strings.TrimSpace(string(token)))
		_ = os.Remove(tokenPath())
	}
	if err != nil {
		return fmt.Errorf("the firewall kept the rules: %v: %s", err, strings.TrimSpace(out))
	}
	return nil
}

func (pfGuard) Holding() bool {
	out, err := pfctl("", "-a", pfAnchor, "-sr")
	return err == nil && strings.Contains(out, guardLabel)
}
