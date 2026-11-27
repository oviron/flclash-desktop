// Package model is what a front end knows, kept in one place: the state of
// the engine and of the helper, what is in progress, the measurements, the
// traffic. Every window and menu reads it and hears when it changes. The
// macOS and Windows apps both run on it, through internal/agent, so the
// rules are written once.
package model

import (
	"os"
	"slices"
	"sync"
	"time"

	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/bridge"
	"github.com/oviron/flclash-desktop/internal/helper"
)

// HistoryLength is how many seconds of traffic the graph shows.
const HistoryLength = 60

// Delay is what measuring a server gave.
type Delay struct {
	// State is "waiting", "ms" or "failed".
	State string `json:"state"`
	MS    int    `json:"ms,omitempty"`
	Error string `json:"error,omitempty"`
}

// Snapshot is the whole state at one moment, as a window draws it.
type Snapshot struct {
	View   app.View      `json:"view"`
	Status bridge.Status `json:"status"`
	// Busy: a change of the tunnel is on its way to the helper.
	Busy bool `json:"busy"`
	// Problem is the last thing that went wrong, until it is dismissed.
	Problem string           `json:"problem,omitempty"`
	Delays  map[string]Delay `json:"delays"`
	// Stats are the traffic of the core while someone watches and something runs.
	Stats *helper.Stats `json:"stats,omitempty"`
	// History is the speeds of the last minute, oldest first: down, up.
	History [][2]int64 `json:"history"`
	// Exit is where the running server lets traffic out, once known.
	Exit *helper.Exit `json:"exit,omitempty"`
	// Updating are the subscriptions being read right now.
	Updating []string `json:"updating"`
	// HelperReady: the helper is there and takes this app's requests.
	HelperReady bool `json:"helperReady"`
	// Test: the app was pointed at a helper or a data folder of its own.
	Test bool `json:"test"`
	// Loaded: the first answers came.
	Loaded bool `json:"loaded"`
}

// Model holds the state and does what the front ends ask.
type Model struct {
	// Call answers an engine request; bridge.Handle unless a test replaces it.
	Call func(bridge.Request) bridge.Reply
	// Lang is the language messages are written in.
	Lang string

	mu         sync.Mutex
	view       app.View
	status     bridge.Status
	busy       bool
	problem    string
	delays     map[string]Delay
	stats      *helper.Stats
	history    [][2]int64
	exit       *helper.Exit
	exitKey    string
	exitFailed bool
	exitAsking bool
	updating   map[string]bool
	watchers   int
	statsBusy  bool
	statsOn    bool
	haveView   bool
	haveStatus bool
	started    bool
	listeners  map[int]func()
	nextID     int

	// Requests go to one of four lines, each served in order:
	// changes of the tunnel, measurements, subscriptions, the counters.
	engine, measuring, fetching, counting *line
	stop                                  chan struct{}
}

// line runs jobs one at a time in the order they came. It never refuses
// one: a job may queue the next while it runs.
type line struct {
	mu   sync.Mutex
	jobs []func()
	wake chan struct{}
}

func newLine() *line {
	l := &line{wake: make(chan struct{}, 1)}
	go func() {
		for {
			l.mu.Lock()
			if len(l.jobs) == 0 {
				l.mu.Unlock()
				<-l.wake
				continue
			}
			job := l.jobs[0]
			l.jobs[0] = nil
			l.jobs = l.jobs[1:]
			l.mu.Unlock()
			job()
		}
	}()
	return l
}

func (l *line) push(job func()) {
	l.mu.Lock()
	l.jobs = append(l.jobs, job)
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// New makes a model; Start sets it going.
func New(lang string) *Model {
	m := &Model{
		Call:      bridge.Handle,
		Lang:      lang,
		delays:    map[string]Delay{},
		updating:  map[string]bool{},
		engine:    newLine(),
		measuring: newLine(),
		fetching:  newLine(),
		counting:  newLine(),
		stop:      make(chan struct{}),
	}
	return m
}

// Start reads the state and keeps it fresh: the helper does not push
// changes, so a slow poll keeps the icon honest; subscriptions whose time
// has come are read at start and then hourly.
func (m *Model) Start() {
	m.Refresh()
	go func() {
		status := time.NewTicker(5 * time.Second)
		due := time.NewTicker(time.Hour)
		stats := time.NewTicker(time.Second)
		defer status.Stop()
		defer due.Stop()
		defer stats.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-status.C:
				m.RefreshStatus()
			case <-due.C:
				m.updateDue()
			case <-stats.C:
				m.readStats()
			}
		}
	}()
	m.updateDue()
}

// OnChange adds a function told of every change and returns what removes
// it. It is called without the lock held, from any goroutine, and must not
// wait.
func (m *Model) OnChange(f func()) (cancel func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listeners == nil {
		m.listeners = map[int]func(){}
	}
	m.nextID++
	id := m.nextID
	m.listeners[id] = f
	return func() {
		m.mu.Lock()
		delete(m.listeners, id)
		m.mu.Unlock()
	}
}

// Snapshot returns the state.
func (m *Model) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{
		View:        m.view,
		Status:      m.status,
		Busy:        m.busy,
		Problem:     m.problem,
		Delays:      make(map[string]Delay, len(m.delays)),
		Stats:       m.stats,
		History:     slices.Clone(m.history),
		Exit:        m.exit,
		Updating:    []string{},
		HelperReady: m.helperReady(),
		Test:        os.Getenv("FLCLASH_HELPER_SOCK") != "" || os.Getenv("FLCLASH_HOME") != "",
		Loaded:      m.haveView && m.haveStatus,
	}
	if s.History == nil {
		s.History = [][2]int64{}
	}
	// A window reads lists, not their absence.
	if s.View.Servers == nil {
		s.View.Servers = []app.Server{}
	}
	if s.View.Policies == nil {
		s.View.Policies = []app.Policy{}
	}
	if s.View.Subscriptions == nil {
		s.View.Subscriptions = []app.Subscription{}
	}
	for id, d := range m.delays {
		s.Delays[id] = d
	}
	for name := range m.updating {
		s.Updating = append(s.Updating, name)
	}
	slices.Sort(s.Updating)
	return s
}

func (m *Model) helperReady() bool { return m.status.Installed && !m.status.Outdated }

func (m *Model) changed() {
	m.mu.Lock()
	listeners := make([]func(), 0, len(m.listeners))
	for _, f := range m.listeners {
		listeners = append(listeners, f)
	}
	m.mu.Unlock()
	for _, f := range listeners {
		f()
	}
}

// call sends a request down a line and hands the reply to then, in order.
func (m *Model) call(l *line, req bridge.Request, then func(bridge.Reply)) {
	req.Lang = m.Lang
	l.push(func() {
		reply := m.Call(req)
		if then != nil {
			then(reply)
		}
	})
}

// take keeps what a reply says. A quiet reply's error is for the caller to
// show, not the model.
func (m *Model) take(reply bridge.Reply, quiet bool) { m.finish(reply, quiet, false) }

// finish keeps what a reply says and, with done, ends the work in progress
// in the same step: nothing sees the work over and the old state.
func (m *Model) finish(reply bridge.Reply, quiet, done bool) {
	m.mu.Lock()
	if done {
		m.busy = false
	}
	if reply.View != nil {
		m.view = *reply.View
		m.haveView = true
	}
	if reply.Status != nil {
		m.status = *reply.Status
		m.haveStatus = true
	}
	if reply.Error != "" && !quiet {
		m.problem = reply.Error
	}
	autoConnect := false
	if !m.started && m.haveView && m.haveStatus {
		m.started = true
		// Once, at start: connect if that is what the person set.
		autoConnect = m.view.AutoConnect && m.helperReady() && !m.status.Running && !m.status.Blocked && m.selected() != nil && !m.busy
	}
	m.mu.Unlock()
	m.askExit()
	m.changed()
	if autoConnect {
		m.Toggle()
	}
}

func (m *Model) selected() *app.Server {
	for i := range m.view.Servers {
		if m.view.Servers[i].ID == m.view.Selected {
			return &m.view.Servers[i]
		}
	}
	return nil
}

func (m *Model) setBusy(busy bool) {
	m.mu.Lock()
	m.busy = busy
	m.mu.Unlock()
}

// Refresh reads the lists and the status.
func (m *Model) Refresh() {
	m.call(m.engine, bridge.Request{Op: "snapshot"}, func(r bridge.Reply) { m.take(r, false) })
	m.RefreshStatus()
}

// RefreshStatus asks the helper what is true now.
func (m *Model) RefreshStatus() {
	m.mu.Lock()
	busy := m.busy
	m.mu.Unlock()
	if busy {
		return
	}
	m.call(m.engine, bridge.Request{Op: "status"}, func(r bridge.Reply) { m.take(r, true) })
}

// Toggle turns the connection on, or off when it runs.
func (m *Model) Toggle() {
	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return
	}
	m.busy = true
	op := "on"
	if m.status.Running {
		op = "off"
	}
	m.mu.Unlock()
	m.changed()
	m.call(m.engine, bridge.Request{Op: op}, func(r bridge.Reply) {
		m.finish(r, false, true)
	})
}

// TurnOff turns everything off, also a blocked state.
func (m *Model) TurnOff() {
	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return
	}
	m.busy = true
	m.mu.Unlock()
	m.changed()
	m.call(m.engine, bridge.Request{Op: "off"}, func(r bridge.Reply) {
		m.finish(r, false, true)
	})
}

// Connect chooses a server and turns the connection on through it.
func (m *Model) Connect(server string) {
	m.setBusy(true)
	m.changed()
	m.call(m.engine, bridge.Request{Op: "select", Server: server}, func(r bridge.Reply) {
		m.mu.Lock()
		running := m.status.Running
		if r.Status != nil {
			running = r.Status.Running
		}
		m.mu.Unlock()
		// A connection that was up has moved already; one that was off starts now.
		if r.Error != "" || running {
			m.finish(r, false, true)
			m.RefreshStatus()
			return
		}
		m.take(r, false)
		m.call(m.engine, bridge.Request{Op: "on"}, func(r bridge.Reply) {
			m.finish(r, false, true)
		})
	})
}

// Change sends a setting or a choice; what runs takes it at once, which
// takes a moment, so it shows as work in progress. Its error is shown, and
// also handed to done when there is one.
func (m *Model) Change(req bridge.Request, quiet bool, done func(bridge.Reply)) {
	m.mu.Lock()
	m.busy = m.status.Running
	m.mu.Unlock()
	m.changed()
	m.call(m.engine, req, func(r bridge.Reply) {
		m.finish(r, quiet, true)
		m.RefreshStatus()
		if done != nil {
			done(r)
		}
	})
}

// Do sends a request that changes nothing that runs: an import, a removal, a
// read. With quiet its error goes to done alone.
func (m *Model) Do(req bridge.Request, quiet bool, done func(bridge.Reply)) {
	m.call(m.engine, req, func(r bridge.Reply) {
		if r.View != nil || r.Status != nil || (r.Error != "" && !quiet) {
			m.take(r, quiet)
		}
		if done != nil {
			done(r)
		}
	})
}

// Aside sends a request on the line of measurements, beside the changes:
// closing a connection, reading the log, a link.
func (m *Model) Aside(req bridge.Request, done func(bridge.Reply)) {
	m.call(m.measuring, req, done)
}

// Subscription sends a request about a subscription; reading one takes as
// long as the provider does, so it has a line of its own.
func (m *Model) Subscription(req bridge.Request, done func(bridge.Reply)) {
	name := req.Name
	if name != "" && (req.Op == "subUpdate" || req.Op == "subEdit") {
		m.mu.Lock()
		if m.updating[name] && req.Op == "subUpdate" {
			m.mu.Unlock()
			// It is being read already; its result comes as a change.
			if done != nil {
				done(bridge.Reply{})
			}
			return
		}
		m.updating[name] = true
		m.mu.Unlock()
		m.changed()
	}
	m.call(m.fetching, req, func(r bridge.Reply) {
		m.mu.Lock()
		delete(m.updating, name)
		m.mu.Unlock()
		m.take(r, true)
		if done != nil {
			done(r)
		}
	})
}

// UpdateAll reads every subscription again.
func (m *Model) UpdateAll() {
	m.mu.Lock()
	var names []string
	for _, s := range m.view.Subscriptions {
		names = append(names, s.Name)
	}
	m.mu.Unlock()
	for _, name := range names {
		m.Subscription(bridge.Request{Op: "subUpdate", Name: name}, nil)
	}
}

func (m *Model) updateDue() {
	m.call(m.fetching, bridge.Request{Op: "subUpdate"}, func(r bridge.Reply) { m.take(r, true) })
}

// Measure measures servers one after another: a connection and a few small
// requests each. Results arrive one by one.
func (m *Model) Measure(servers []string) {
	m.mu.Lock()
	var fresh []string
	for _, id := range servers {
		if m.delays[id].State != "waiting" {
			fresh = append(fresh, id)
			m.delays[id] = Delay{State: "waiting"}
		}
	}
	m.mu.Unlock()
	if len(fresh) == 0 {
		return
	}
	m.changed()
	left := len(fresh)
	for _, id := range fresh {
		m.call(m.measuring, bridge.Request{Op: "delay", Server: id}, func(r bridge.Reply) {
			m.mu.Lock()
			if r.Delay > 0 {
				m.delays[id] = Delay{State: "ms", MS: r.Delay}
			} else {
				m.delays[id] = Delay{State: "failed", Error: r.Error}
			}
			left--
			last := left == 0
			m.mu.Unlock()
			m.changed()
			// A measurement may have learned where servers let traffic out.
			if last {
				m.Refresh()
			}
		})
	}
}

// DismissProblem forgets the last problem.
func (m *Model) DismissProblem() {
	m.mu.Lock()
	m.problem = ""
	m.mu.Unlock()
	m.changed()
}

// WatchStats is called with true by every window that shows traffic when it
// appears and with false when it goes; the counters are read only while
// someone watches.
func (m *Model) WatchStats(on bool) {
	m.mu.Lock()
	if on {
		m.watchers++
	} else if m.watchers > 0 {
		m.watchers--
	}
	was := m.statsOn
	m.statsOn = m.watchers > 0
	if !m.statsOn {
		m.stats, m.history = nil, nil
	}
	first := m.statsOn && !was
	if first {
		// An address that could not be found out is asked for again when
		// someone opens a window to look.
		m.exitFailed = false
	}
	m.mu.Unlock()
	if first {
		m.readStats()
		m.askExit()
	}
	m.changed()
}

func (m *Model) readStats() {
	m.mu.Lock()
	if !m.statsOn {
		m.mu.Unlock()
		return
	}
	if !m.status.Running {
		had := m.stats != nil
		m.stats, m.history = nil, nil
		m.mu.Unlock()
		if had {
			m.changed()
		}
		return
	}
	if m.statsBusy || m.busy {
		m.mu.Unlock()
		return
	}
	m.statsBusy = true
	m.mu.Unlock()
	m.counting.push(func() {
		r := m.Call(bridge.Request{Op: "stats", Lang: m.Lang})
		m.mu.Lock()
		m.statsBusy = false
		if !m.statsOn {
			m.mu.Unlock()
			return
		}
		m.stats = r.Stats
		if r.Stats != nil {
			m.history = append(m.history, [2]int64{r.Stats.Down, r.Stats.Up})
			if over := len(m.history) - HistoryLength; over > 0 {
				m.history = m.history[over:]
			}
		}
		m.mu.Unlock()
		m.changed()
	})
}

// askExit asks for the exit address when what runs has changed. It is asked
// once per connection, and only while something on screen shows it.
func (m *Model) askExit() {
	m.mu.Lock()
	key := ""
	if m.status.Running && m.status.Captured {
		key = m.status.Server + "|" + time.Unix(m.status.Since, 0).String() + "|" + m.status.Mode
	}
	if key != m.exitKey {
		m.exitKey, m.exit, m.exitFailed = key, nil, false
	}
	if key == "" || m.exit != nil || m.exitFailed || m.watchers == 0 || m.exitAsking {
		m.mu.Unlock()
		return
	}
	m.exitAsking = true
	m.mu.Unlock()
	m.call(m.measuring, bridge.Request{Op: "exit"}, func(r bridge.Reply) {
		m.mu.Lock()
		m.exitAsking = false
		// The answer is about what ran when it was asked.
		if m.exitKey != key {
			m.mu.Unlock()
			return
		}
		m.exit = r.Exit
		m.exitFailed = r.Exit == nil
		refresh := false
		if r.Exit != nil && r.Exit.Country != "" {
			if s := m.selected(); s != nil && s.Country != r.Exit.Country {
				refresh = true
			}
		}
		m.mu.Unlock()
		m.changed()
		// The engine kept the country for the list as well.
		if refresh {
			m.Refresh()
		}
	})
}
