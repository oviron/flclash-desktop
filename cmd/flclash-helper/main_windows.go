// flclash-helper is the privileged supervisor: a Windows service that runs as
// the system. It listens on a named pipe only the given user can open and runs
// the core.
//
//	flclash-helper -sid SID -core C:\path\to\flclash-core.exe
package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"

	winio "github.com/tailscale/go-winio"
	"golang.org/x/sys/windows/svc"

	"github.com/oviron/flclash-desktop/internal/helper"
)

// ServiceName is the name the helper is installed under.
const ServiceName = "FlClashHelper"

func main() {
	sid := flag.String("sid", "", "the user allowed to give orders, as a SID")
	corePath := flag.String("core", "", `path to flclash-core.exe`)
	flag.Parse()
	if *sid == "" || *corePath == "" {
		log.Fatal(`usage: flclash-helper -sid SID -core PATH`)
	}
	asService, err := svc.IsWindowsService()
	if err != nil {
		log.Fatal(err)
	}
	if !asService {
		// Run in the foreground, for a look at it from a console.
		h, err := start(*sid, *corePath, os.Stderr)
		if err != nil {
			log.Fatal(err)
		}
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)
		<-stop
		h.stop()
		return
	}
	if err := svc.Run(ServiceName, &service{sid: *sid, core: *corePath}); err != nil {
		log.Fatal(err)
	}
}

type running struct {
	sup      *helper.Supervisor
	listener net.Listener
	logFile  *os.File
}

func (r *running) stop() {
	// The core goes, the guard comes off last and the system proxy is put
	// back: a helper that stops leaves the network as it found it.
	r.sup.Stop()
	r.listener.Close()
	if r.logFile != nil {
		r.logFile.Close()
	}
}

func start(sid, corePath string, console io.Writer) (*running, error) {
	if err := helper.PrepareDirs(sid); err != nil {
		return nil, err
	}
	r := &running{}
	var out io.Writer = console
	if console == nil {
		// The log is one file that only grows; a helper that starts finds it
		// large and starts it anew.
		if info, err := os.Stat(helper.LogPath()); err == nil && info.Size() > 8<<20 {
			_ = os.Truncate(helper.LogPath(), 0)
		}
		file, err := os.OpenFile(helper.LogPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return nil, err
		}
		r.logFile, out = file, file
	}
	log.SetOutput(out)
	listener, err := winio.ListenPipe(helper.SocketPath(), &winio.PipeConfig{SecurityDescriptor: helper.PipeSDDL(sid)})
	if err != nil {
		return nil, err
	}
	r.listener = listener
	r.sup = &helper.Supervisor{
		CorePath:    corePath,
		TunnelOnly:  true,
		Guard:       helper.NewWindowsGuard(corePath),
		SystemProxy: helper.NewWindowsProxy(sid, corePath),
		UserSID:     sid,
		Log:         out,
	}
	go func() {
		log.Printf("listening on %s for %s", helper.SocketPath(), sid)
		if err := r.sup.Serve(listener); err != nil {
			log.Printf("serve: %v", err)
		}
	}()
	return r, nil
}

type service struct {
	sid  string
	core string
}

func (s *service) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	r, err := start(s.sid, s.core, nil)
	if err != nil {
		log.Printf("start: %v", err)
		return true, 1
	}
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for req := range requests {
		switch req.Cmd {
		case svc.Interrogate:
			status <- req.CurrentStatus
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			r.stop()
			return false, 0
		}
	}
	return false, 0
}
