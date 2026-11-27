// Package agent serves the model to the windows of a desktop app that runs
// them as processes of their own, and to a window in its own process. On
// Windows the agent is the process in the notification area: it holds the
// engine and the model, and the window is a separate program that comes and
// goes. They talk over a local connection, one JSON object per line each way.
// On macOS the window links the engine, and Local serves it the same messages
// without a connection.
//
// A window says "hello" first; from then on it is sent the whole state each
// time something changes, and it sends what the person does. Any other
// client, such as a second start of the app, may send a single action.
package agent

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"sync"
	"time"

	"github.com/oviron/flclash-desktop/internal/bridge"
	"github.com/oviron/flclash-desktop/internal/model"
)

// Message is what a client sends.
type Message struct {
	// ID, when set, is repeated in the reply to this message.
	ID int `json:"id,omitempty"`
	// Action is one of:
	//   hello                 this client is a window; state follows
	//   open {page}           show the window, at a page if given
	//   toggle, turnOff, refresh, updateAll, dismiss
	//   connect {server}      choose a server and turn on through it
	//   measure {servers}
	//   watch {on}            traffic is on screen, or no longer
	//   change|do|aside|subscription {request, quiet}
	//                         an engine request on that line; replied to
	//   installHelper         asks the system to install the helper; replied to
	//   hotKey {hotKey}       the shortcut that connects; replied to
	//   login {on}            start with the system; replied to
	//   quit
	Action  string          `json:"action"`
	Server  string          `json:"server,omitempty"`
	Servers []string        `json:"servers,omitempty"`
	On      bool            `json:"on,omitempty"`
	Page    string          `json:"page,omitempty"`
	Quiet   bool            `json:"quiet,omitempty"`
	Request *bridge.Request `json:"request,omitempty"`
	HotKey  *HotKey         `json:"hotKey,omitempty"`
}

// Event is what a client is sent.
type Event struct {
	ID int `json:"id,omitempty"`
	// Event is "model" (the state), "reply" (to a message with an ID) or
	// "activate" (come to the front, at Page if given).
	Event string          `json:"event"`
	Model *model.Snapshot `json:"model,omitempty"`
	App   *App            `json:"app,omitempty"`
	Reply *bridge.Reply   `json:"reply,omitempty"`
	Page  string          `json:"page,omitempty"`
}

// HotKey is a system-wide shortcut: the key and modifiers as the system
// numbers them, and how it is written for people. Empty Title is none.
type HotKey struct {
	Key       uint32 `json:"key"`
	Modifiers uint32 `json:"modifiers"`
	Title     string `json:"title"`
}

// App is what the agent itself keeps, beside the engine.
type App struct {
	// Lang is the language the app shows, as the strings files name it.
	Lang   string `json:"lang"`
	HotKey HotKey `json:"hotKey"`
	Login  bool   `json:"login"`
	// Version is the version of the app, for the about box.
	Version string `json:"version"`
}

// Agent answers clients.
type Agent struct {
	Model *model.Model
	// App returns the agent's own settings.
	App func() App
	// Open shows the window, at page if not empty. It is called when no
	// window is connected, or always, to let the host start one.
	Open func(page string)
	// InstallHelper asks for the helper to be installed and waits for it.
	InstallHelper func() error
	SetHotKey     func(HotKey) error
	SetLogin      func(on bool) error
	Quit          func()
	// A host that does one of these itself leaves it nil, and the action
	// is refused.

	mu      sync.Mutex
	windows map[*client]bool
}

type client struct {
	agent *Agent
	// write sends one event; writing is held by wmu.
	write func(Event)
	wmu   sync.Mutex

	mu sync.Mutex
	// dirty: the state changed since it was last sent. replies wait for
	// the writer; wake tells it there is something.
	dirty   bool
	replies []Event
	wake    chan struct{}
	watches int
	window  bool
}

func newClient(a *Agent, write func(Event)) *client {
	return &client{agent: a, write: write, wake: make(chan struct{}, 1)}
}

func (c *client) send(e Event) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.write(e)
}

func (c *client) poke() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// reply queues a reply behind the state it follows from.
func (c *client) reply(e Event) {
	c.mu.Lock()
	c.replies = append(c.replies, e)
	c.mu.Unlock()
	c.poke()
}

// Serve answers the clients that connect until the listener is closed.
func (a *Agent) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go a.serve(conn)
	}
}

// Activate tells the windows to come to the front; it reports whether there
// was one to tell.
func (a *Agent) Activate(page string) bool {
	a.mu.Lock()
	var windows []*client
	for c := range a.windows {
		windows = append(windows, c)
	}
	a.mu.Unlock()
	for _, c := range windows {
		c.send(Event{Event: "activate", Page: page})
	}
	return len(windows) > 0
}

// Changed tells the windows that what App returns has changed.
func (a *Agent) Changed() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for c := range a.windows {
		c.mark()
	}
}

func (c *client) mark() {
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
	c.poke()
}

func (a *Agent) serve(conn net.Conn) {
	enc := json.NewEncoder(conn)
	c := newClient(a, func(e Event) {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if enc.Encode(e) != nil {
			conn.Close()
		}
	})
	defer conn.Close()
	done := make(chan struct{})
	go c.push(done)
	reader := bufio.NewReaderSize(conn, 64*1024)
	var stop func()
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var m Message
			if json.Unmarshal(line, &m) == nil {
				if m.Action == "hello" && !c.window {
					stop = c.hello()
				}
				a.handle(c, m)
			}
		}
		if err != nil {
			break
		}
	}
	c.gone(stop)
	close(done)
}

// hello makes the client a window: it is sent the state from now on.
func (c *client) hello() (stop func()) {
	a := c.agent
	c.mu.Lock()
	c.window = true
	c.mu.Unlock()
	a.mu.Lock()
	if a.windows == nil {
		a.windows = map[*client]bool{}
	}
	a.windows[c] = true
	a.mu.Unlock()
	stop = a.Model.OnChange(c.mark)
	c.mark()
	return stop
}

// gone forgets a client; a window that went away stopped watching.
func (c *client) gone(stop func()) {
	a := c.agent
	if stop != nil {
		stop()
	}
	a.mu.Lock()
	delete(a.windows, c)
	a.mu.Unlock()
	c.mu.Lock()
	watches := c.watches
	c.watches = 0
	c.mu.Unlock()
	for range watches {
		a.Model.WatchStats(false)
	}
}

// push sends the state when it changed, at most every few hundredths of a
// second, so a burst of changes is drawn once, and the replies. A reply is
// sent after the state it changed: the model keeps a reply before it hands
// it on, so the change is marked before the reply is queued.
func (c *client) push(done chan struct{}) {
	for {
		select {
		case <-done:
			return
		case <-c.wake:
		}
		c.mu.Lock()
		dirty := c.dirty && c.window
		c.dirty = false
		replies := c.replies
		c.replies = nil
		c.mu.Unlock()
		if dirty {
			snapshot := c.agent.Model.Snapshot()
			app := c.agent.App()
			c.send(Event{Event: "model", Model: &snapshot, App: &app})
		}
		for _, e := range replies {
			c.send(e)
		}
		if dirty {
			select {
			case <-done:
				return
			case <-time.After(30 * time.Millisecond):
			}
		}
	}
}

// Local serves a window in this process: emit is handed every event as
// JSON, from one goroutine at a time, and send takes the window's messages.
// The window is a window from the start, without a hello; stop ends it.
func (a *Agent) Local(emit func([]byte)) (send func([]byte), stop func()) {
	c := newClient(a, func(e Event) {
		if b, err := json.Marshal(e); err == nil {
			emit(b)
		}
	})
	done := make(chan struct{})
	go c.push(done)
	cancel := c.hello()
	send = func(b []byte) {
		var m Message
		if err := json.Unmarshal(b, &m); err != nil {
			c.reply(Event{Event: "reply", Reply: &bridge.Reply{Error: "bad message"}})
			return
		}
		a.handle(c, m)
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			c.gone(cancel)
			close(done)
		})
	}
	return send, stop
}

func (a *Agent) handle(c *client, m Message) {
	reply := func(r bridge.Reply) {
		if m.ID != 0 {
			c.reply(Event{ID: m.ID, Event: "reply", Reply: &r})
		}
	}
	errReply := func(err error) {
		r := bridge.Reply{}
		if err != nil {
			r.Error = err.Error()
		}
		reply(r)
	}
	model := a.Model
	refused := func(hook bool) bool {
		if !hook {
			reply(bridge.Reply{Error: "not done by the agent here: " + m.Action})
		}
		return !hook
	}
	switch m.Action {
	case "hello":
		reply(bridge.Reply{})
	case "open":
		if !refused(a.Open != nil) {
			a.Open(m.Page)
		}
	case "toggle":
		model.Toggle()
	case "turnOff":
		model.TurnOff()
	case "refresh":
		model.Refresh()
	case "updateAll":
		model.UpdateAll()
	case "dismiss":
		model.DismissProblem()
	case "connect":
		model.Connect(m.Server)
	case "measure":
		model.Measure(m.Servers)
	case "watch":
		c.mu.Lock()
		if !m.On && c.watches == 0 {
			c.mu.Unlock()
			return
		}
		if m.On {
			c.watches++
		} else {
			c.watches--
		}
		c.mu.Unlock()
		model.WatchStats(m.On)
	case "change", "do", "aside", "subscription":
		if m.Request == nil {
			reply(bridge.Reply{Error: "no request"})
			return
		}
		switch m.Action {
		case "change":
			model.Change(*m.Request, m.Quiet, reply)
		case "do":
			model.Do(*m.Request, m.Quiet, reply)
		case "aside":
			model.Aside(*m.Request, reply)
		default:
			model.Subscription(*m.Request, reply)
		}
	case "installHelper":
		if refused(a.InstallHelper != nil) {
			return
		}
		go func() {
			err := a.InstallHelper()
			model.Refresh()
			errReply(err)
		}()
	case "hotKey":
		if refused(a.SetHotKey != nil) {
			return
		}
		if m.HotKey == nil {
			m.HotKey = &HotKey{}
		}
		err := a.SetHotKey(*m.HotKey)
		a.Changed()
		errReply(err)
	case "login":
		if refused(a.SetLogin != nil) {
			return
		}
		err := a.SetLogin(m.On)
		a.Changed()
		errReply(err)
	case "quit":
		if !refused(a.Quit != nil) {
			a.Quit()
		}
	default:
		reply(bridge.Reply{Error: "unknown action " + m.Action})
	}
}

// Send connects to an agent through dial and sends one action, as a second
// start of the app does to bring up the first.
func Send(conn io.ReadWriteCloser, m Message) error {
	defer conn.Close()
	return json.NewEncoder(conn).Encode(m)
}
