package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.10.0", "0.9.0", true},
		{"0.9.0", "0.10.0", false},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.0-rc.1", true},
		{"1.0.0-rc.1", "1.0.0", false},
		{"1.0.0-rc.2", "1.0.0-rc.1", true},
		{"1.0.0-rc.10", "1.0.0-rc.2", true},
		{"1.0.0-beta", "1.0.0-alpha", true},
		{"1.0.0-alpha.1", "1.0.0-alpha", true},
		{"0.4.0", "0.3.0-5-gabcdef", true},
		{"dev", "0.1.0", false},
		{"0.1.0", "dev", false},
		{"01.0.0", "0.1.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// fake is a GitHub with one release whose sums are signed by priv.
type fake struct {
	priv    ed25519.PrivateKey
	tag     string
	file    []byte
	name    string
	sums    string
	sig     string
	release string
}

func newFake(t *testing.T, tag, name string, file []byte) (*fake, *httptest.Server) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	_ = pub
	f := &fake{priv: priv, tag: tag, file: file, name: name}
	sum := sha256.Sum256(file)
	f.sums = hex.EncodeToString(sum[:]) + "  " + name + "\n" + strings.Repeat("0", 64) + "  other.zip\n"
	f.sig = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(f.sums)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + Repo + "/releases/latest":
			base := "http://" + r.Host
			json.NewEncoder(w).Encode(map[string]any{
				"tag_name": f.tag, "html_url": base + "/page",
				"assets": []map[string]any{
					{"name": f.name, "browser_download_url": base + "/file", "size": len(f.file)},
					{"name": "SHA256SUMS", "browser_download_url": base + "/sums"},
					{"name": "SHA256SUMS.sig", "browser_download_url": base + "/sig"},
				},
			})
		case "/file":
			w.Write(f.file)
		case "/sums":
			w.Write([]byte(f.sums))
		case "/sig":
			w.Write([]byte(f.sig))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func updater(t *testing.T, srv *httptest.Server, f *fake, current string) (*Updater, *string) {
	t.Helper()
	applied := new(string)
	u := &Updater{
		Current: current,
		Release: true,
		Dir:     t.TempDir(),
		Asset:   func(v string) string { return "FlClash-" + v + ".zip" },
		Apply:   func(path, version string) error { *applied = path; return nil },
		api:     srv.URL,
		key:     f.priv.Public().(ed25519.PublicKey),
	}
	u.Start()
	return u, applied
}

func TestCheckAndInstall(t *testing.T) {
	file := []byte("the new app")
	f, srv := newFake(t, "v0.4.0", "FlClash-0.4.0.zip", file)
	u, applied := updater(t, srv, f, "0.3.0")
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := u.State()
	if s.Latest != "0.4.0" || s.Page != srv.URL+"/page" || s.Checked == 0 || s.Error != "" {
		t.Fatalf("state after check: %+v", s)
	}
	if err := u.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(*applied)
	if err != nil || string(got) != string(file) {
		t.Fatalf("applied %q: %q, %v", *applied, got, err)
	}

	// What was found survives a restart.
	again := &Updater{Current: "0.3.0", Release: true, Dir: u.Dir}
	again.Start()
	if again.State().Latest != "0.4.0" {
		t.Fatalf("restart forgot the release: %+v", again.State())
	}
}

func TestCheckNothingNewer(t *testing.T) {
	f, srv := newFake(t, "v0.3.0", "FlClash-0.3.0.zip", []byte("x"))
	u, _ := updater(t, srv, f, "0.3.0")
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := u.State(); s.Latest != "" || s.Checked == 0 {
		t.Fatalf("state: %+v", s)
	}
}

func TestUnsignedReleaseIsNotOffered(t *testing.T) {
	f, srv := newFake(t, "v0.4.0", "FlClash-0.4.0.zip", []byte("x"))
	f.sig = ""
	u, _ := updater(t, srv, f, "0.3.0")
	unsigned := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.4.0", "html_url": "x", "assets": []any{}})
	}))
	defer unsigned.Close()
	u.api = unsigned.URL
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := u.State(); s.Latest != "" {
		t.Fatalf("offered an unsigned release: %+v", s)
	}
}

func TestBadlySignedReleaseIsNotOffered(t *testing.T) {
	f, srv := newFake(t, "v0.4.0", "FlClash-0.4.0.zip", []byte("x"))
	_, other, _ := ed25519.GenerateKey(nil)
	f.sig = base64.StdEncoding.EncodeToString(ed25519.Sign(other, []byte(f.sums)))
	u, _ := updater(t, srv, f, "0.3.0")
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := u.State(); s.Latest != "" || s.Checked == 0 {
		t.Fatalf("offered a release signed with another key: %+v", s)
	}
}

func TestInstallRefusesWhatIsNotSigned(t *testing.T) {
	for name, spoil := range map[string]func(*fake){
		"file":      func(f *fake) { f.file = []byte("another program") },
		"sums":      func(f *fake) { f.sums = strings.Replace(f.sums, "other", "else", 1) },
		"signature": func(f *fake) { f.sig = base64.StdEncoding.EncodeToString(make([]byte, 64)) },
		"key": func(f *fake) {
			_, other, _ := ed25519.GenerateKey(nil)
			f.sig = base64.StdEncoding.EncodeToString(ed25519.Sign(other, []byte(f.sums)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, srv := newFake(t, "v0.4.0", "FlClash-0.4.0.zip", []byte("the new app"))
			u, applied := updater(t, srv, f, "0.3.0")
			spoil(f)
			if err := u.Install(context.Background()); err == nil || *applied != "" {
				t.Fatalf("installed a spoiled release: err %v, applied %q", err, *applied)
			}
			if s := u.State(); s.Installing || s.Error == "" {
				t.Fatalf("state: %+v", s)
			}
		})
	}
}

func TestHandBuiltCopyDoesNotCheck(t *testing.T) {
	u := &Updater{Current: "0.3.0-4-gabc", Dir: t.TempDir()}
	u.Start()
	if err := u.Check(context.Background()); err == nil {
		t.Fatal("a hand-built copy checked")
	}
	if u.State().Enabled {
		t.Fatal("a hand-built copy is enabled")
	}

	// Nor does it install what a release copy found before in the same place.
	os.WriteFile(u.path(), []byte(`{"latest": "9.0.0", "page": "x"}`), 0o600)
	applied := false
	u = &Updater{
		Current: "0.3.0-4-gabc",
		Dir:     u.Dir,
		Asset:   func(v string) string { return "FlClash-" + v + ".zip" },
		Apply:   func(string, string) error { applied = true; return nil },
	}
	u.Start()
	if s := u.State(); s.Latest != "" || s.CanInstall {
		t.Fatalf("a hand-built copy offers an update: %+v", s)
	}
	if err := u.Install(context.Background()); err == nil || applied {
		t.Fatal("a hand-built copy installed")
	}
}

func TestPublicKeyReads(t *testing.T) {
	k, err := base64.StdEncoding.DecodeString(PublicKey)
	if err != nil || len(k) != ed25519.PublicKeySize {
		t.Fatalf("PublicKey is not an Ed25519 key: %v", err)
	}
}
