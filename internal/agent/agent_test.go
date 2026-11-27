package agent

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/oviron/flclash-desktop/internal/app"
	"github.com/oviron/flclash-desktop/internal/bridge"
	"github.com/oviron/flclash-desktop/internal/model"
)

func TestWindow(t *testing.T) {
	m := model.New("en")
	m.Call = func(r bridge.Request) bridge.Reply {
		switch r.Op {
		case "snapshot":
			return bridge.Reply{View: &app.View{Selected: "a"}}
		case "status":
			return bridge.Reply{Status: &bridge.Status{Installed: true}}
		case "link":
			return bridge.Reply{Link: "vless://" + r.Server}
		}
		return bridge.Reply{}
	}
	opened := make(chan string, 1)
	a := &Agent{Model: m, App: func() App { return App{Lang: "en"} }, Open: func(p string) { opened <- p }}
	ours, theirs := net.Pipe()
	go a.serve(theirs)
	defer ours.Close()
	enc := json.NewEncoder(ours)
	in := bufio.NewScanner(ours)
	next := func() Event {
		t.Helper()
		ours.SetReadDeadline(time.Now().Add(3 * time.Second))
		if !in.Scan() {
			t.Fatalf("no event: %v", in.Err())
		}
		var e Event
		if err := json.Unmarshal(in.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	go enc.Encode(Message{Action: "hello"})
	m.Refresh()
	loaded := false
	for !loaded {
		e := next()
		loaded = e.Event == "model" && e.Model.Loaded && e.Model.View.Selected == "a"
	}
	go enc.Encode(Message{ID: 7, Action: "aside", Request: &bridge.Request{Op: "link", Server: "x"}})
	for {
		e := next()
		if e.Event == "reply" {
			if e.ID != 7 || e.Reply.Link != "vless://x" {
				t.Fatalf("reply %+v", e)
			}
			break
		}
	}
	go enc.Encode(Message{Action: "open", Page: "settings"})
	select {
	case p := <-opened:
		if p != "settings" {
			t.Fatal(p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not opened")
	}
}

// A window in the process hears the state before the reply that changed it,
// and actions the host does itself are refused, not lost.
func TestLocal(t *testing.T) {
	m := model.New("en")
	m.Call = func(r bridge.Request) bridge.Reply {
		switch r.Op {
		case "import":
			return bridge.Reply{View: &app.View{Selected: r.Text}}
		case "status":
			return bridge.Reply{Status: &bridge.Status{Installed: true}}
		}
		return bridge.Reply{}
	}
	a := &Agent{Model: m, App: func() App { return App{Lang: "en"} }}
	events := make(chan Event, 100)
	send, stop := a.Local(func(b []byte) {
		var e Event
		if err := json.Unmarshal(b, &e); err != nil {
			t.Error(err)
		}
		events <- e
	})
	defer stop()
	next := func() Event {
		t.Helper()
		select {
		case e := <-events:
			return e
		case <-time.After(3 * time.Second):
			t.Fatal("no event")
		}
		return Event{}
	}
	for i := range 20 {
		text := string(rune('a' + i))
		b, _ := json.Marshal(Message{ID: i + 1, Action: "do", Request: &bridge.Request{Op: "import", Text: text}})
		send(b)
		seen := ""
		for {
			e := next()
			if e.Event == "model" {
				seen = e.Model.View.Selected
				continue
			}
			if e.ID != i+1 {
				t.Fatalf("reply %+v", e)
			}
			break
		}
		if seen != text {
			t.Fatalf("reply %d came before its state: saw %q", i+1, seen)
		}
	}
	send([]byte(`{"id":99,"action":"quit"}`))
	for {
		e := next()
		if e.Event == "reply" {
			if e.ID != 99 || e.Reply.Error == "" {
				t.Fatalf("quit: %+v", e)
			}
			break
		}
	}
}
