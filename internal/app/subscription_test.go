package app

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != subscriptionAgent {
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
