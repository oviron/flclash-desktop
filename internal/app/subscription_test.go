package app

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/oviron/flclash-desktop/internal/policy"
	"github.com/oviron/flclash-desktop/internal/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("FLCLASH_HOME", t.TempDir())
	// No helper in tests: asking it must fail fast, not reach a real one.
	t.Setenv("FLCLASH_HELPER_SOCK", t.TempDir()+"/none.sock")
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func names(t *testing.T, st *store.Store, group string) string {
	t.Helper()
	view, err := Snapshot(st)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range view.Servers {
		if s.Group == group {
			out = append(out, s.Name)
		}
	}
	return strings.Join(out, ",")
}

func TestSubscription(t *testing.T) {
	body := ""
	status := http.StatusOK
	wantAgent := subscriptionAgent
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != wantAgent {
			t.Errorf("got the agent %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Profile-Update-Interval", "6")
		w.Header().Set("Subscription-Userinfo", "upload=100; download=900; total=5000; expire=2000000000")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
	}))
	defer provider.Close()
	st := open(t)

	body = "vless://u@a.example:443?security=reality&pbk=K#one\nhy2://p@b.example:443#two\nwireguard://x@c.example:1#three\nhy2://p@d.example:443#two\n"
	result, err := AddSubscription(st, "work", provider.URL+"/sub?token=SECRET")
	if err != nil || len(result.Imported) != 3 || len(result.Failed) != 1 {
		t.Fatalf("got %+v, %v", result, err)
	}
	if got := names(t, st, "work"); got != "one,two,two (2)" {
		t.Fatalf("got %q", got)
	}
	view, _ := Snapshot(st)
	sub := view.Subscriptions[0]
	if sub.Name != "work" || strings.Contains(sub.Host, "SECRET") || sub.Used != 1000 || sub.Total != 5000 || sub.Expire != 2000000000 || sub.Updated == 0 {
		t.Fatalf("got %+v", sub)
	}
	var one string
	for _, s := range view.Servers {
		if s.Name == "one" {
			one = s.ID
		}
	}
	if err := Select(st, one, ""); err != nil {
		t.Fatal(err)
	}

	// The provider renames nothing and drops a server: IDs and the choice stay.
	body = "vless://u@a.example:443?security=reality&pbk=K2#one\nhy2://p@e.example:443#four\n"
	if _, err := UpdateSubscription(st, "work"); err != nil {
		t.Fatal(err)
	}
	if got := names(t, st, "work"); got != "one,four" {
		t.Fatalf("got %q", got)
	}
	if view, _ = Snapshot(st); view.Selected != one {
		t.Fatalf("the chosen server kept its name and must keep its place: %q", view.Selected)
	}

	// A provider that is down, or serves rubbish, takes nothing away.
	for _, broken := range []func(){
		func() { status = http.StatusBadGateway },
		func() { status = http.StatusOK; body = "" },
		func() { body = "wireguard://x@c.example:1#three\n" },
	} {
		broken()
		if _, err := UpdateSubscription(st, "work"); err == nil {
			t.Fatal("the update must fail")
		}
		if got := names(t, st, "work"); got != "one,four" {
			t.Fatalf("a failed update changed the servers: %q", got)
		}
		if view, _ = Snapshot(st); view.Subscriptions[0].Problem == "" || strings.Contains(view.Subscriptions[0].Problem, "SECRET") {
			t.Fatalf("got the problem %q", view.Subscriptions[0].Problem)
		}
	}

	// Settings: another client name, an interval, words that leave servers out.
	status, body = http.StatusOK, "hy2://p@a.example:443#Amsterdam\nhy2://p@b.example:443#Moscow x2\nhy2://p@c.example:443#Info: 30 days left\n"
	wantAgent = "clash-verge/v2"
	if _, err := EditSubscription(st, "work", "", "clash-verge/v2", 3, "info:, MOSCOW"); err != nil {
		t.Fatal(err)
	}
	if got := names(t, st, "work"); got != "Amsterdam" {
		t.Fatalf("got %q", got)
	}
	if view, _ = Snapshot(st); view.Subscriptions[0].Every != 3 || view.Subscriptions[0].Agent != "clash-verge/v2" {
		t.Fatalf("got %+v", view.Subscriptions[0])
	}
	if _, err := EditSubscription(st, "work", "ftp://x", "", 0, ""); err == nil {
		t.Fatal("a bad address must be refused")
	}

	if _, err := AddSubscription(st, "work", provider.URL); err == nil {
		t.Fatal("a second subscription of the same name must be refused")
	}
	if _, err := AddSubscription(st, "x", "file:///etc/passwd"); err == nil {
		t.Fatal("only web addresses are subscriptions")
	}
	if err := RemoveSubscription(st, "work"); err != nil {
		t.Fatal(err)
	}
	if view, _ = Snapshot(st); len(view.Subscriptions) != 0 || len(view.Servers) != 0 || view.Selected != "" {
		t.Fatalf("got %+v", view)
	}
}

func TestANewSetupChangesNothingInTheSystem(t *testing.T) {
	view, err := Snapshot(open(t))
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != "proxy" || view.AutoConnect {
		t.Fatalf("a new setup must only open the local port, and only when asked: %+v", view)
	}
}

func TestPolicyDocuments(t *testing.T) {
	st := open(t)
	doc, err := ReadPolicy(st, "ru-direct")
	if err != nil || !doc.Builtin || !doc.Listed || doc.Rest != "PROXY" || !strings.Contains(doc.Direct, "geosite:yandex") {
		t.Fatalf("got %+v, %v", doc, err)
	}
	if err := WritePolicyLists(st, "mine", policy.Lists{Direct: strings.Split(doc.Direct, "\n"), Block: []string{"ads.example"}, Rest: doc.Rest}); err != nil {
		t.Fatal(err)
	}
	mine, err := ReadPolicy(st, "mine")
	if err != nil || mine.Builtin || !mine.Listed || mine.Block != "ads.example" || mine.Direct != doc.Direct {
		t.Fatalf("got %+v, %v", mine, err)
	}
	// Rules in an order of their own are kept as written and not shown as lists.
	if err := WritePolicy(st, "mine", "DOMAIN-SUFFIX,a.example,PROXY\nDOMAIN-SUFFIX,example,DIRECT\nMATCH,PROXY\n"); err != nil {
		t.Fatal(err)
	}
	if mine, _ = ReadPolicy(st, "mine"); mine.Listed || !strings.Contains(mine.Text, "a.example") {
		t.Fatalf("got %+v", mine)
	}
	if err := WritePolicy(st, "ru-direct", "MATCH,PROXY\n"); err == nil {
		t.Fatal("a built-in policy must not be overwritten")
	}
	if err := WritePolicyLists(st, "mine", policy.Lists{Direct: []string{"two words"}, Rest: "PROXY"}); err == nil {
		t.Fatal("a bad entry must refuse the save")
	}
}

func TestBackup(t *testing.T) {
	st := open(t)
	if _, err := Import(st, "default", "hy2://p@a.example:443#one\nhy2://p@b.example:443#two\n"); err != nil {
		t.Fatal(err)
	}
	if err := WritePolicy(st, "mine", "DOMAIN-SUFFIX,corp.example,DIRECT\nMATCH,PROXY\n"); err != nil {
		t.Fatal(err)
	}
	if err := Select(st, "two", "mine"); err != nil {
		t.Fatal(err)
	}
	file := t.TempDir() + "/backup.json"
	if err := WriteBackup(st, file); err != nil {
		t.Fatal(err)
	}
	before, _ := Snapshot(st)

	// Another setup takes its place, then the backup comes back.
	other := open(t)
	if _, err := Import(other, "default", "hy2://p@z.example:443#zed\n"); err != nil {
		t.Fatal(err)
	}
	if err := WritePolicy(other, "theirs", "MATCH,DIRECT\n"); err != nil {
		t.Fatal(err)
	}
	if err := ReadBackup(other, file); err != nil {
		t.Fatal(err)
	}
	after, _ := Snapshot(other)
	if len(after.Servers) != 2 || after.Selected != before.Selected || after.Policy != "mine" || names(t, other, "default") != "one,two" {
		t.Fatalf("got %+v", after)
	}
	if _, err := ReadPolicy(other, "theirs"); err == nil {
		t.Fatal("a policy that is not in the backup must be gone")
	}

	// A file that is not a backup, or holds what cannot be used, changes nothing.
	for _, text := range []string{"{}", "not json", `{"flclashBackup":1,"state":{"servers":[{"id":"x","name":"bad","proxy":{"type":"ssh","server":"a"}}]},"policies":{}}`} {
		bad := t.TempDir() + "/bad.json"
		_ = os.WriteFile(bad, []byte(text), 0o600)
		if err := ReadBackup(other, bad); err == nil {
			t.Errorf("%q must be refused", text)
		}
		if names(t, other, "default") != "one,two" {
			t.Fatal("a refused restore changed the servers")
		}
	}
}

// A change saved while a provider is being read stays: what the provider
// gave goes into the state as it is when the answer comes.
func TestSubscriptionReadKeepsChangesMadeMeanwhile(t *testing.T) {
	release := make(chan struct{})
	first := true
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !first {
			<-release
		}
		first = false
		_, _ = w.Write([]byte("hy2://p@b.example:443#two\n"))
	}))
	defer provider.Close()
	st := open(t)
	if _, err := AddSubscription(st, "work", provider.URL); err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() {
		_, err := UpdateSubscription(st, "work")
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	if err := SetAutoConnect(st, true); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	state, _ := st.Load()
	if !state.AutoConnect {
		t.Fatal("the change made meanwhile was lost")
	}
	if got := names(t, st, "work"); got != "two" {
		t.Fatalf("got %q", got)
	}
}

func TestSubscriptionTitleFromHeaders(t *testing.T) {
	cases := []struct {
		name        string
		title       string
		disposition string
		explicit    string
		want        string
	}{
		{"base64", "base64:" + base64.StdEncoding.EncodeToString([]byte("Provider One")), `attachment; filename="Ignored.txt"`, "", "Provider One"},
		{"plain", "Plain Name", "", "", "Plain Name"},
		{"disposition", "", `attachment; filename="FromFile.yaml"`, "", "FromFile"},
		{"explicit wins", "Header Name", "", "My Choice", "My Choice"},
		{"fallback host", "", "", "", "127.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.title != "" {
					w.Header().Set("Profile-Title", tc.title)
				}
				if tc.disposition != "" {
					w.Header().Set("Content-Disposition", tc.disposition)
				}
				_, _ = w.Write([]byte("hy2://p@a.example:443#one\n"))
			}))
			defer srv.Close()
			st := open(t)
			if _, err := AddSubscription(st, tc.explicit, srv.URL); err != nil {
				t.Fatal(err)
			}
			view, _ := Snapshot(st)
			if len(view.Subscriptions) != 1 || view.Subscriptions[0].Name != tc.want {
				t.Fatalf("got %+v, want name %q", view.Subscriptions, tc.want)
			}
		})
	}
}
