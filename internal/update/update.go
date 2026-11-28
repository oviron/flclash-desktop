// Package update finds a newer release of the app on GitHub, fetches the
// file this copy installs from, makes sure the project signed it, and hands
// it to the system to install.
//
// A release carries SHA256SUMS and SHA256SUMS.sig, an Ed25519 signature of
// the sums made with the project's key ("make sign-release", on the
// maintainer's computer: the key is not given to CI). The app
// trusts a file only when the signature checks against the key built into it
// and the file's hash is the one the sums name: HTTPS and GitHub alone would
// let anyone who can change a release's files hand out another program.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oviron/flclash-desktop/internal/say"
)

// Repo is where the releases are.
const Repo = "oviron/flclash-desktop"

// PublicKey is the project's Ed25519 key, base64, that signs the sums of
// every release. The private half stays in the maintainer's keychain.
const PublicKey = "HnFXzGzU8cO/aOglkDdKzBJ42mI7a3aZ7RjgdRx5P84="

// Every is how often an automatic check runs.
const Every = 24 * time.Hour

// State is what the windows show.
type State struct {
	// Current is the version that runs.
	Current string `json:"current"`
	// Enabled: this copy is a release and can look for the next one. A copy
	// built by hand has no release to follow.
	Enabled bool `json:"enabled"`
	// Auto: it looks once a day by itself.
	Auto     bool `json:"auto"`
	Checking bool `json:"checking"`
	// Latest is a newer version that is out, with its page; empty when this
	// one is the newest known.
	Latest string `json:"latest,omitempty"`
	Page   string `json:"page,omitempty"`
	// Checked is when GitHub last answered, in Unix seconds; 0 is never.
	Checked int64 `json:"checked,omitempty"`
	// CanInstall: this copy installs the update itself; otherwise the page
	// is opened for the person to download it.
	CanInstall bool `json:"canInstall"`
	Installing bool `json:"installing"`
	// Error is what went wrong with the last check or install.
	Error string `json:"error,omitempty"`
}

// Updater keeps the state and runs the checks.
type Updater struct {
	// Current is the version that runs; Release says it came from a tag
	// through the release workflow.
	Current string
	Release bool
	// Dir is where the state file and the downloads go.
	Dir string
	// Lang is the language of the errors.
	Lang string
	// Asset names the release file this copy installs from, for a version;
	// "" when it cannot install itself.
	Asset func(version string) string
	// Apply starts installing the verified file and quits the app; it
	// returns only on failure.
	Apply func(path, version string) error
	// Proxy, when set, gives the local port to go through while the app is
	// connected: GitHub may be reachable only through the server.
	Proxy func() string
	// Changed is told when the state changes.
	Changed func()

	// api is the GitHub API, a test server in tests.
	api string
	key ed25519.PublicKey

	mu    sync.Mutex
	state State
	saved saved
	// saving keeps writes of the state file in order.
	saving sync.Mutex
}

// saved is what survives a restart.
type saved struct {
	Auto    *bool  `json:"auto,omitempty"`
	Checked int64  `json:"checked,omitempty"`
	Latest  string `json:"latest,omitempty"`
	Page    string `json:"page,omitempty"`
}

func (u *Updater) path() string { return filepath.Join(u.Dir, "update.json") }

// Start reads what was saved and checks once a day while auto is on.
func (u *Updater) Start() {
	if u.api == "" {
		u.api = "https://api.github.com"
	}
	if u.key == nil {
		if k, err := base64.StdEncoding.DecodeString(PublicKey); err == nil && len(k) == ed25519.PublicKeySize {
			u.key = k
		}
	}
	if b, err := os.ReadFile(u.path()); err == nil {
		_ = json.Unmarshal(b, &u.saved)
	}
	u.mu.Lock()
	u.state = State{
		Current:    u.Current,
		Enabled:    u.Release,
		Auto:       u.saved.Auto == nil || *u.saved.Auto,
		Checked:    u.saved.Checked,
		CanInstall: u.Release && u.Asset != nil && u.Asset(u.Current) != "",
	}
	if u.Release && Newer(u.saved.Latest, u.Current) {
		u.state.Latest, u.state.Page = u.saved.Latest, u.saved.Page
	}
	u.mu.Unlock()
	if !u.Release {
		return
	}
	go func() {
		// Not in the first seconds: the app has its own work at start.
		time.Sleep(30 * time.Second)
		// The installer a previous update downloaded has done its work by
		// now; it is not left to take space.
		u.mu.Lock()
		if !u.state.Installing {
			_ = os.RemoveAll(filepath.Join(u.Dir, "update"))
		}
		u.mu.Unlock()
		for {
			u.mu.Lock()
			due := u.state.Auto && time.Since(time.Unix(u.state.Checked, 0)) >= Every
			u.mu.Unlock()
			if due {
				_ = u.Check(context.Background())
			}
			// Spread a little, so copies started together do not ask together.
			time.Sleep(time.Hour + time.Duration(rand.IntN(600))*time.Second)
		}
	}()
}

// State is the state now.
func (u *Updater) State() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

func (u *Updater) change(f func(*State)) {
	u.mu.Lock()
	f(&u.state)
	u.mu.Unlock()
	if u.Changed != nil {
		u.Changed()
	}
}

func (u *Updater) save() {
	u.saving.Lock()
	defer u.saving.Unlock()
	u.mu.Lock()
	auto := u.state.Auto
	u.saved.Auto = &auto
	u.saved.Checked = u.state.Checked
	u.saved.Latest = u.state.Latest
	u.saved.Page = u.state.Page
	b, _ := json.MarshalIndent(u.saved, "", "  ")
	u.mu.Unlock()
	_ = os.MkdirAll(u.Dir, 0o700)
	// Written aside and moved in place: a write cut short leaves the old
	// file, not half of one.
	tmp := u.path() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, u.path())
	}
}

// SetAuto turns the daily check on or off.
func (u *Updater) SetAuto(on bool) {
	u.change(func(s *State) { s.Auto = on })
	u.save()
}

func (u *Updater) fail(err error) error {
	text := say.In(u.Lang, err)
	u.change(func(s *State) { s.Checking, s.Installing, s.Error = false, false, text })
	return errors.New(text)
}

// release is the part of GitHub's answer the app reads.
type release struct {
	Tag        string  `json:"tag_name"`
	Page       string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

func (r release) find(name string) *asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// errUnsigned: the release has no sums signed with the project's key.
var errUnsigned = say.New("the release is not signed with the project's key")

// sums fetches the release's sums and returns them once their signature
// checks; errUnsigned when there is none or it does not check.
func (u *Updater) sums(ctx context.Context, r release) ([]byte, error) {
	sums, sig := r.find("SHA256SUMS"), r.find("SHA256SUMS.sig")
	if sums == nil || sig == nil {
		return nil, errUnsigned
	}
	sumsText, err := u.get(ctx, sums.URL, 64<<10)
	if err != nil {
		return nil, err
	}
	sigText, err := u.get(ctx, sig.URL, 4<<10)
	if err != nil {
		return nil, err
	}
	if !signed(u.key, sumsText, sigText) {
		return nil, errUnsigned
	}
	return sumsText, nil
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

func (u *Updater) client() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(r *http.Request) (*url.URL, error) {
		if u.Proxy != nil {
			if hostPort := u.Proxy(); hostPort != "" {
				return &url.URL{Scheme: "http", Host: hostPort}, nil
			}
		}
		// Not connected: a proxy the system's environment names, if any.
		return http.ProxyFromEnvironment(r)
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Minute}
}

func (u *Updater) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FlClash/"+u.Current)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, say.Errorf("GitHub did not answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, say.Errorf("GitHub answered %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func (u *Updater) latest(ctx context.Context) (release, error) {
	var r release
	b, err := u.get(ctx, u.api+"/repos/"+Repo+"/releases/latest", 1<<20)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, say.Errorf("GitHub's answer does not read: %w", err)
	}
	if r.Draft || r.Prerelease || !strings.HasPrefix(r.Tag, "v") || parse(r.Tag[1:]) == nil {
		return release{}, say.New("the latest release is not a version this app follows")
	}
	return r, nil
}

// Check asks GitHub for the latest release.
func (u *Updater) Check(ctx context.Context) error {
	if !u.Release {
		return u.fail(say.New("this copy was built by hand and has no releases to follow"))
	}
	u.mu.Lock()
	if u.state.Checking || u.state.Installing {
		u.mu.Unlock()
		return nil
	}
	u.state.Checking = true
	u.mu.Unlock()
	u.change(func(s *State) { s.Error = "" })
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := u.latest(ctx)
	if err != nil {
		return u.fail(err)
	}
	version := r.Tag[1:]
	offer := false
	if Newer(version, u.Current) {
		// The sums are signed after the release is published, on the
		// maintainer's computer; until then there is nothing to offer.
		switch _, err := u.sums(ctx, r); {
		case err == nil:
			offer = true
		case !errors.Is(err, errUnsigned):
			return u.fail(err)
		}
	}
	u.change(func(s *State) {
		s.Checking = false
		s.Checked = time.Now().Unix()
		s.Latest, s.Page = "", ""
		if offer {
			s.Latest, s.Page = version, r.Page
		}
	})
	u.save()
	return nil
}

// Install fetches the newest release's file, checks it and starts installing
// it; the app quits for that. A copy that cannot install itself reports so.
func (u *Updater) Install(ctx context.Context) error {
	u.mu.Lock()
	if u.state.Installing || u.state.Checking {
		u.mu.Unlock()
		return nil
	}
	if !u.state.CanInstall {
		u.mu.Unlock()
		return u.fail(say.New("this copy does not install updates itself: download the new one from its page"))
	}
	u.state.Installing = true
	u.mu.Unlock()
	u.change(func(s *State) { s.Error = "" })
	r, err := u.latest(ctx)
	if err != nil {
		return u.fail(err)
	}
	version := r.Tag[1:]
	if !Newer(version, u.Current) {
		u.change(func(s *State) { s.Installing, s.Latest, s.Page = false, "", "" })
		u.save()
		return nil
	}
	path, err := u.fetch(ctx, r, u.Asset(version))
	if err != nil {
		return u.fail(err)
	}
	if err := u.Apply(path, version); err != nil {
		return u.fail(say.Errorf("the update did not start: %w", err))
	}
	return nil
}

// fetch downloads the named file of the release into Dir and returns its
// path once the signature and the hash check.
func (u *Updater) fetch(ctx context.Context, r release, name string) (string, error) {
	file := r.find(name)
	if file == nil {
		return "", say.Errorf("the release has no %s", name)
	}
	sums, err := u.sums(ctx, r)
	if err != nil {
		return "", err
	}
	want, err := hashOf(sums, name)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(u.Dir, "update")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "FlClash/"+u.Current)
	resp, err := u.client().Do(req)
	if err != nil {
		return "", say.Errorf("GitHub did not answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", say.Errorf("GitHub answered %s", resp.Status)
	}
	path := filepath.Join(dir, name)
	out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	// The file is a few tens of megabytes; a much larger answer is not it.
	n, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(resp.Body, 512<<20))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", say.Errorf("the download broke off: %w", err)
	}
	if file.Size > 0 && n != file.Size {
		return "", say.New("the download broke off")
	}
	if hex.EncodeToString(hash.Sum(nil)) != want {
		os.Remove(path)
		return "", say.New("the downloaded file is not the one the release signed")
	}
	return path, nil
}

// signed reports whether sig, base64, is key's signature of sums.
func signed(key ed25519.PublicKey, sums, sig []byte) bool {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	return len(key) == ed25519.PublicKeySize && err == nil && ed25519.Verify(key, sums, raw)
}

// hashOf is the SHA-256, in hex, the sums give the named file.
func hashOf(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name && len(fields[0]) == 64 {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", say.Errorf("the signed sums do not name %s", name)
}

// Newer reports whether version a comes after b, by Semantic Versioning: a
// pre-release comes before its release.
func Newer(a, b string) bool {
	x, y := parse(a), parse(b)
	if x == nil || y == nil {
		return false
	}
	for i := range 3 {
		if x.n[i] != y.n[i] {
			return x.n[i] > y.n[i]
		}
	}
	switch {
	case x.pre == y.pre:
		return false
	case x.pre == "":
		return true
	case y.pre == "":
		return false
	}
	return comparePre(x.pre, y.pre) > 0
}

type semver struct {
	n   [3]int
	pre string
}

func parse(v string) *semver {
	v, _, _ = strings.Cut(v, "+")
	core, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return nil
	}
	var s semver
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return nil
		}
		s.n[i] = n
	}
	s.pre = pre
	return &s
}

// comparePre orders pre-release fields: numbers by value and below words.
func comparePre(a, b string) int {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		m, errM := strconv.Atoi(x[i])
		n, errN := strconv.Atoi(y[i])
		switch {
		case errM == nil && errN == nil && m != n:
			if m > n {
				return 1
			}
			return -1
		case errM == nil && errN != nil:
			return -1
		case errM != nil && errN == nil:
			return 1
		case x[i] != y[i] && (errM != nil || errN != nil):
			return strings.Compare(x[i], y[i])
		}
	}
	return len(x) - len(y)
}
