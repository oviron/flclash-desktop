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
	"github.com/oviron/flclash-desktop/internal/say"
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
	Agent   string `json:"agent"`
	Every   int    `json:"every"`
	Skip    string `json:"skip"`
}

func subscriptions(state store.State) []Subscription {
	out := []Subscription{}
	for _, sub := range state.Subscriptions {
		view := Subscription{Name: sub.Name, Used: sub.Used, Total: sub.Total, Expire: sub.Expire, Problem: sub.Problem,
			Agent: sub.Agent, Every: sub.Every, Skip: sub.Skip}
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

func fetch(address, agent string) (fetched, error) {
	if agent == "" {
		agent = subscriptionAgent
	}
	var out fetched
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return out, say.New("the address does not read as one")
	}
	req.Header.Set("User-Agent", agent)
	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// The error of the client quotes the address, and the address is a secret.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return out, say.Errorf("the provider did not answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, say.Errorf("the provider answered %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return out, say.Errorf("the answer broke off: %w", err)
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
			// A date past what calendars write is not one.
			if n > 0 && n <= 253402300799 {
				out.expire = n
			}
		}
	}
	return out, nil
}

// AddSubscription names an address as the source of a new group and reads it.
// A subscription that gave no server is not kept.
func AddSubscription(st *store.Store, name, address string) (ImportResult, error) {
	name, address = strings.TrimSpace(name), strings.TrimSpace(address)
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ImportResult{}, say.New("a subscription address starts with https://")
	}
	if name == "" {
		name = u.Hostname()
	}
	taken := func(state store.State) error {
		for _, sub := range state.Subscriptions {
			if sub.Name == name {
				return say.Errorf("there is a subscription named %q already", name)
			}
		}
		for _, e := range state.Servers {
			if e.Group == name {
				return say.Errorf("the group %q has servers that were added by hand; choose another name", name)
			}
		}
		return nil
	}
	state, err := st.Load()
	if err != nil {
		return ImportResult{}, err
	}
	if err := taken(state); err != nil {
		return ImportResult{}, err
	}
	// The provider is asked with nothing held: it may take a while.
	got, err := fetch(address, "")
	if err != nil {
		return ImportResult{Imported: []Server{}, Failed: []Failure{}}, err
	}
	var result ImportResult
	err = st.Update(func(state *store.State) error {
		if err := taken(*state); err != nil {
			return err
		}
		state.Subscriptions = append(state.Subscriptions, store.Subscription{Name: name, URL: address})
		result, err = update(state, len(state.Subscriptions)-1, got)
		return err
	})
	return result, err
}

// errMoved: the subscription was changed while it was being read; what was
// read is of the old one and is let go.
var errMoved = errors.New("the subscription changed meanwhile")

// UpdateSubscription reads a subscription again. When that fails the servers
// it had stay, and the reason is kept for the list to show.
func UpdateSubscription(st *store.Store, name string) (ImportResult, error) {
	state, err := st.Load()
	if err != nil {
		return ImportResult{}, err
	}
	i := subscriptionIndex(state, name)
	if i < 0 {
		return ImportResult{}, say.Errorf("no subscription %q", name)
	}
	asked := state.Subscriptions[i]
	// The provider is asked with nothing held, and what it gave goes into
	// the state as it is then: a change saved meanwhile stays.
	got, fetchErr := fetch(asked.URL, asked.Agent)
	var result ImportResult
	var readErr error
	err = st.Update(func(state *store.State) error {
		i := subscriptionIndex(*state, name)
		if i < 0 {
			return say.Errorf("no subscription %q", name)
		}
		if sub := state.Subscriptions[i]; sub.URL != asked.URL || sub.Agent != asked.Agent || sub.Skip != asked.Skip {
			return errMoved
		}
		if readErr = fetchErr; readErr == nil {
			result, readErr = update(state, i, got)
		}
		if readErr != nil {
			state.Subscriptions[i].Problem = say.Text(readErr)
		}
		return nil
	})
	if errors.Is(err, errMoved) {
		return ImportResult{Imported: []Server{}, Failed: []Failure{}}, nil
	}
	if err != nil {
		return result, err
	}
	return result, readErr
}

func subscriptionIndex(state store.State, name string) int {
	for i := range state.Subscriptions {
		if state.Subscriptions[i].Name == name {
			return i
		}
	}
	return -1
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
		hours := sub.Every
		if hours <= 0 {
			hours = sub.Hours
		}
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

// EditSubscription changes how a subscription is read: the address, when a
// new one is given; the client name told to the provider; how often, in
// hours, zero leaving it to the provider; and the words that leave servers
// out. Then it reads the subscription again.
func EditSubscription(st *store.Store, name, address, agent string, every int, skip string) (ImportResult, error) {
	address, agent = strings.TrimSpace(address), strings.TrimSpace(agent)
	if address != "" {
		u, err := url.Parse(address)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return ImportResult{}, say.New("a subscription address starts with https://")
		}
	}
	if every < 0 || every > 24*30 {
		return ImportResult{}, say.New("the interval is a number of hours, up to a month")
	}
	if strings.ContainsAny(agent, "\r\n") || len(agent) > 200 {
		return ImportResult{}, say.New("the client name is one short line")
	}
	err := st.Update(func(state *store.State) error {
		i := subscriptionIndex(*state, name)
		if i < 0 {
			return say.Errorf("no subscription %q", name)
		}
		sub := &state.Subscriptions[i]
		if address != "" {
			sub.URL = address
		}
		sub.Agent, sub.Every, sub.Skip = agent, every, strings.TrimSpace(skip)
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return UpdateSubscription(st, name)
}

// RemoveSubscription deletes a subscription and its servers.
func RemoveSubscription(st *store.Store, name string) error {
	return st.Update(func(state *store.State) error {
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
			return say.Errorf("no subscription %q", name)
		}
		kept := state.Servers[:0]
		for _, e := range state.Servers {
			if e.Group != name {
				kept = append(kept, e)
				continue
			}
			if e.ID == state.Selected {
				if status, err := Status(); err == nil && status.Running {
					return say.New("the tunnel runs through a server of this subscription: turn it off or pick another first")
				}
				state.Selected = ""
			}
		}
		state.Subscriptions, state.Servers = subs, kept
		return nil
	})
}

// update puts what the subscription at index i gave into the state: its servers
// replace the ones the group had, and a server that keeps its name keeps its ID.
func update(state *store.State, i int, got fetched) (ImportResult, error) {
	result := ImportResult{Imported: []Server{}, Failed: []Failure{}}
	sub := &state.Subscriptions[i]
	parsed, refused, n := ingest.ParseAll(got.body)
	for _, r := range refused {
		result.Failed = append(result.Failed, Failure{Entry: r.Entry, Reason: r.Reason})
	}
	var servers []ingest.Server
	names := map[string]int{}
	var skip []string
	for _, word := range strings.Split(sub.Skip, ",") {
		if word = strings.ToLower(strings.TrimSpace(word)); word != "" {
			skip = append(skip, word)
		}
	}
	skipped := 0
next:
	for _, server := range parsed {
		for _, word := range skip {
			if strings.Contains(strings.ToLower(server.Name), word) {
				skipped++
				continue next
			}
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
			return result, say.New("the provider gave an empty list")
		}
		if len(result.Failed) == 0 {
			return result, say.Errorf("all %d servers were left out by the words to skip", skipped)
		}
		return result, say.Errorf("none of the %d entries could be read; the first: %s", n, result.Failed[0].Reason)
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
