// Package agent serves the model to the windows of a desktop app that runs
// them as processes of their own. On Windows the agent is the process in the
// notification area: it holds the engine and the model, and the window is a
// separate program that comes and goes. They talk over a local connection,
// one JSON object per line each way.
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

	mu      sync.Mutex
	windows map[*client]bool
}

type client struct {
	agent   *Agent
	conn    net.Conn
	mu      sync.Mutex
	enc     *json.Encoder
	dirty   chan struct{}
	watches int
	window  bool
}

func (c *client) send(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if c.enc.Encode(e) != nil {
		c.conn.Close()
	}
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
	select {
	case c.dirty <- struct{}{}:
	default:
	}
}

func (a *Agent) serve(conn net.Conn) {
	c := &client{agent: a, conn: conn, enc: json.NewEncoder(conn), dirty: make(chan struct{}, 1)}
	defer conn.Close()
	reader := bufio.NewReaderSize(conn, 64*1024)
	var stop func()
	done := make(chan struct{})
	defer close(done)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var m Message
			if json.Unmarshal(line, &m) == nil {
				if m.Action == "hello" && !c.window {
					c.window = true
					a.mu.Lock()
					if a.windows == nil {
						a.windows = map[*client]bool{}
					}
					a.windows[c] = true
					a.mu.Unlock()
					stop = a.Model.OnChange(c.mark)
					go c.push(done)
					c.mark()
				}
				a.handle(c, m)
			}
		}
		if err != nil {
			break
		}
	}
	if stop != nil {
		stop()
	}
	a.mu.Lock()
	delete(a.windows, c)
	a.mu.Unlock()
	// A window that went away stopped watching.
	c.mu.Lock()
	watches := c.watches
	c.watches = 0
	c.mu.Unlock()
	for range watches {
		a.Model.WatchStats(false)
	}
}

// push sends the state when it changed, at most every few hundredths of a
// second: a burst of changes is drawn once.
func (c *client) push(done chan struct{}) {
	for {
		select {
		case <-done:
			return
		case <-c.dirty:
			snapshot := c.agent.Model.Snapshot()
			app := c.agent.App()
			c.send(Event{Event: "model", Model: &snapshot, App: &app})
			time.Sleep(30 * time.Millisecond)
		}
	}
}

func (a *Agent) handle(c *client, m Message) {
	reply := func(r bridge.Reply) {
		if m.ID != 0 {
			c.send(Event{ID: m.ID, Event: "reply", Reply: &r})
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
	switch m.Action {
	case "hello":
		reply(bridge.Reply{})
	case "open":
		a.Open(m.Page)
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
		go func() {
			err := a.InstallHelper()
			model.Refresh()
			errReply(err)
		}()
	case "hotKey":
		if m.HotKey == nil {
			m.HotKey = &HotKey{}
		}
		err := a.SetHotKey(*m.HotKey)
		a.Changed()
		errReply(err)
	case "login":
		err := a.SetLogin(m.On)
		a.Changed()
		errReply(err)
	case "quit":
		a.Quit()
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
