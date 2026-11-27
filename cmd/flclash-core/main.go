// flclash-core is the core process the helper runs. It reads frames from
// stdin: a length as four big-endian bytes, one byte for the kind, a body.
// A config frame is applied; a delay frame measures one server. Every frame is
// answered with a JSON line on descriptor 3. Stdout stays free for the log of
// the core.
// When stdin closes, which also happens if the helper dies, it shuts the core
// down and exits, and the tunnel goes with it.
//
// The answers go to descriptor 3, or where -verdicts says: on Windows a
// child is handed a pipe by its handle.
//
// With -delay or -exit it does one thing and exits: reads the request from
// stdin, measures the delay or asks where the server lets traffic out, writes
// the answer to stdout. That needs no privileges; the app runs it so while
// the tunnel is off.
package main

import (
	"cmp"
	"encoding/binary"
	"encoding/json"
	"flag"
	"io"
	"os"
	"time"

	"github.com/metacubex/mihomo/log"

	"github.com/oviron/flclash-desktop/internal/core"
	"github.com/oviron/flclash-desktop/internal/helper"
	"github.com/oviron/flclash-desktop/internal/say"
)

func main() {
	if otherMode() {
		return
	}
	// The helper says where the core keeps its files: a core that runs as the
	// user cannot write where root's does.
	home := flag.String("home", helper.StateDir(), "where the core keeps its files")
	verdicts := flag.Uint64("verdicts", 3, "the descriptor, or on Windows the handle, the answers go to")
	delayOnce := flag.Bool("delay", false, "measure the server on stdin and exit")
	exitOnce := flag.Bool("exit", false, "ask through the server on stdin where it lets traffic out, and exit")
	lang := flag.String("lang", "", "the language of the answers' errors, as the helper's requests say it")
	flag.Parse()
	if *delayOnce || *exitOnce {
		// Stdout carries the answer alone.
		log.SetLevel(log.SILENT)
		body, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		reply := helper.CoreReply{}
		if err != nil {
			reply.Error = err.Error()
		} else if *exitOnce {
			reply = exit(body, *lang)
		} else {
			reply = delay(body, *lang)
		}
		_ = json.NewEncoder(os.Stdout).Encode(reply)
		return
	}

	out := json.NewEncoder(os.NewFile(uintptr(*verdicts), "verdicts"))
	for {
		var size [4]byte
		if _, err := io.ReadFull(os.Stdin, size[:]); err != nil {
			break
		}
		frame := make([]byte, binary.BigEndian.Uint32(size[:]))
		if _, err := io.ReadFull(os.Stdin, frame); err != nil || len(frame) == 0 {
			break
		}
		var reply helper.CoreReply
		switch kind, body := frame[0], frame[1:]; kind {
		case helper.FrameConfig:
			if err := core.Apply(*home, body); err != nil {
				reply.Error = say.In(*lang, err)
			}
		case helper.FrameDelay:
			reply = delay(body, *lang)
		case helper.FrameExit:
			reply = exit(body, *lang)
		case helper.FrameClose:
			if !core.Close(string(body)) {
				reply.Error = say.In(*lang, say.New("the connection is gone already"))
			}
		case helper.FrameStats:
			stats := core.Stats()
			reply.Stats = &stats
		default:
			reply.Error = "unknown frame"
		}
		if err := out.Encode(reply); err != nil {
			break
		}
	}
	core.Stop()
}

// delay measures one server. An error reads in the language of the request,
// or else in the one the core was started with.
func delay(body []byte, lang string) helper.CoreReply {
	var req helper.CoreDelay
	if err := json.Unmarshal(body, &req); err != nil {
		return helper.CoreReply{Error: "bad delay request: " + err.Error()}
	}
	ms, err := core.Delay(req.Proxy, req.URL, time.Duration(req.TimeoutMS)*time.Millisecond)
	if err != nil {
		return helper.CoreReply{Error: say.In(cmp.Or(req.Lang, lang), err)}
	}
	// A delay of zero would read as no answer.
	return helper.CoreReply{Delay: max(ms, 1)}
}

func exit(body []byte, lang string) helper.CoreReply {
	var req helper.CoreDelay
	if err := json.Unmarshal(body, &req); err != nil {
		return helper.CoreReply{Error: "bad exit request: " + err.Error()}
	}
	exit, err := core.Exit(req.Proxy, req.URL, time.Duration(req.TimeoutMS)*time.Millisecond)
	if err != nil {
		return helper.CoreReply{Error: say.In(cmp.Or(req.Lang, lang), err)}
	}
	return helper.CoreReply{Exit: &exit}
}
