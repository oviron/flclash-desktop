package app

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/oviron/flclash-desktop/internal/ingest"
	"github.com/oviron/flclash-desktop/internal/store"
)

// Subscription is a subscription as a front end shows it. The address is a
// secret, so only its host is shown.
type Subscription struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Updated int64  `json:"updated"` // unix seconds, 0 when never
	Used    int64  `json:"used"`
	Total   int64  `json:"total"`
	Expire  int64  `json:"expire"`
	Problem string `json:"problem"`
}

func subscriptions(state store.State) []Subscription {
	out := []Subscription{}
	for _, sub := range state.Subscriptions {
		view := Subscription{Name: sub.Name, Used: sub.Used, Total: sub.Total, Expire: sub.Expire, Problem: sub.Problem}
		if u, err := url.Parse(sub.URL); err == nil {
			view.Host = u.Host
		}
		if !sub.Updated.IsZero() {
			view.Updated = sub.Updated.Unix()
		}
		out = append(out, view)
	}
	return out
}

// The client a provider sees. Providers pick the format by this name; one
// they do not know gets the plain list of links, which is what is read here.
const subscriptionAgent = "FlClash-Desktop/0.1"

// defaultHours is how often a subscription is read when the provider did not say.
const defaultHours = 12

// fetched is what a subscription address answered.
type fetched struct {
	body                string
	hours               int
	used, total, expire int64
}

func fetch(address string) (fetched, error) {
	var out fetched
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return out, errors.New("the address does not read as one")
	}
	req.Header.Set("User-Agent", subscriptionAgent)
	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// The error of the client quotes the address, and the address is a secret.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return out, fmt.Errorf("the provider did not answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("the provider answered %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return out, fmt.Errorf("the answer broke off: %w", err)
	}
	out.body = string(raw)
	out.hours, _ = strconv.Atoi(strings.TrimSpace(resp.Header.Get("Profile-Update-Interval")))
	// upload=1; download=2; total=3; expire=4
	for _, part := range strings.Split(resp.Header.Get("Subscription-Userinfo"), ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if !ok || err != nil {
			continue
		}
		switch key {
		case "upload", "download":
			out.used += n
		case "total":
			out.total = n
		case "expire":
			out.expire = n
		}
	}
	return out, nil
}

// AddSubscription names an address as the source of a new group and reads it.
// A subscription that gave no server is not kept.
func AddSubscription(st *store.Store, name, address string) (ImportResult, error) {
	name, address = strings.TrimSpace(name), strings.TrimSpace(address)
	state, err := st.Load()
	if err != nil {
		return ImportResult{}, err
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ImportResult{}, errors.New("a subscription address starts with https://")
	}
	if name == "" {
		name = u.Hostname()
	}
	for _, sub := range state.Subscriptions {
		if sub.Name == name {
			return ImportResult{}, fmt.Errorf("there is a subscription named %q already", name)
		}
	}
	for _, e := range state.Servers {
		if e.Group == name {
			return ImportResult{}, fmt.Errorf("the group %q has servers that were added by hand; choose another name", name)
		}
	}
	state.Subscriptions = append(state.Subscriptions, store.Subscription{Name: name, URL: address})
	result, err := update(&state, len(state.Subscriptions)-1)
	if err != nil {
		return result, err
	}
	return result, st.Save(state)
}

// UpdateSubscription reads a subscription again. When that fails the servers
// it had stay, and the reason is kept for the list to show.
func UpdateSubscription(st *store.Store, name string) (ImportResult, error) {
	state, err := st.Load()
	if err != nil {
		return ImportResult{}, err
	}
	for i := range state.Subscriptions {
		if state.Subscriptions[i].Name != name {
			continue
		}
		result, err := update(&state, i)
		if err != nil {
			state.Subscriptions[i].Problem = err.Error()
		}
		if saveErr := st.Save(state); saveErr != nil {
			return result, saveErr
		}
		return result, err
	}
	return ImportResult{}, fmt.Errorf("no subscription %q", name)
}

// UpdateDue reads again every subscription whose time has come, and says how
// many it read. One that fails does not stop the others.
func UpdateDue(st *store.Store) (int, error) {
	state, err := st.Load()
	if err != nil {
		return 0, err
	}
	done := 0
	for _, sub := range state.Subscriptions {
		hours := sub.Hours
		if hours <= 0 {
			hours = defaultHours
		}
		if time.Since(sub.Updated) < time.Duration(hours)*time.Hour {
			continue
		}
		_, _ = UpdateSubscription(st, sub.Name)
		done++
	}
	return done, nil
}

// RemoveSubscription deletes a subscription and its servers.
func RemoveSubscription(st *store.Store, name string) error {
	state, err := st.Load()
	if err != nil {
		return err
	}
	found := false
	subs := state.Subscriptions[:0]
	for _, sub := range state.Subscriptions {
		if sub.Name == name {
			found = true
			continue
		}
		subs = append(subs, sub)
	}
	if !found {
		return fmt.Errorf("no subscription %q", name)
	}
	kept := state.Servers[:0]
	for _, e := range state.Servers {
		if e.Group != name {
			kept = append(kept, e)
			continue
		}
		if e.ID == state.Selected {
			if status, err := Status(); err == nil && status.Running {
				return errors.New("the tunnel runs through a server of this subscription: turn it off or pick another first")
			}
			state.Selected = ""
		}
	}
	state.Subscriptions, state.Servers = subs, kept
	return st.Save(state)
}

// update reads the subscription at index i into the state: its servers
// replace the ones the group had, and a server that keeps its name keeps its ID.
func update(state *store.State, i int) (ImportResult, error) {
	result := ImportResult{Imported: []Server{}, Failed: []Failure{}}
	sub := &state.Subscriptions[i]
	got, err := fetch(sub.URL)
	if err != nil {
		return result, err
	}
	text := got.body
	if lines, ok := ingest.IsSubscriptionBody(text); ok {
		text = lines
	}
	var servers []ingest.Server
	names := map[string]int{}
	n := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n++
		server, err := ingest.Parse(line)
		if err != nil {
			result.Failed = append(result.Failed, Failure{Entry: n, Reason: err.Error()})
			continue
		}
		// Two servers of one name would be one row; the later ones get a number.
		names[server.Name]++
		if count := names[server.Name]; count > 1 {
			server.Name = fmt.Sprintf("%s (%d)", server.Name, count)
		}
		servers = append(servers, server)
	}
	if len(servers) == 0 {
		if n == 0 {
			return result, errors.New("the provider gave an empty list")
		}
		return result, fmt.Errorf("none of the %d entries could be read; the first: %s", n, result.Failed[0].Reason)
	}

	fresh := map[string]bool{}
	for _, server := range servers {
		e := state.Put(sub.Name, server)
		fresh[e.ID] = true
		result.Imported = append(result.Imported, Server{ID: e.ID, Name: e.Name, Group: e.Group, Protocol: protocol(e.Proxy)})
	}
	kept := state.Servers[:0]
	for _, e := range state.Servers {
		if e.Group == sub.Name && !fresh[e.ID] {
			// A server the provider took away. A tunnel that runs through it
			// goes on until it is turned off; the choice is just gone.
			if e.ID == state.Selected {
				state.Selected = ""
			}
			continue
		}
		kept = append(kept, e)
	}
	state.Servers = kept
	sub.Updated, sub.Problem = time.Now(), ""
	sub.Hours, sub.Used, sub.Total, sub.Expire = got.hours, got.used, got.total, got.expire
	return result, nil
}
