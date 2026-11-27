// flclash-helper is the privileged supervisor. The system starts it as root;
// it listens on a socket only the given user can reach and runs the core.
//
//	flclash-helper -uid UID -core /path/to/flclash-core
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/oviron/flclash-desktop/internal/helper"
)

func main() {
	uid := flag.Int("uid", -1, "the user allowed to give orders")
	corePath := flag.String("core", "", "path to flclash-core")
	flag.Parse()
	if *uid < 0 || *corePath == "" {
		log.Fatal("usage: flclash-helper -uid UID -core PATH")
	}
	if err := os.MkdirAll(helper.StateDir(), 0o700); err != nil {
		log.Fatal(err)
	}
	path := helper.SocketPath()
	_ = os.Remove(path) // a socket left by a previous run
	// Nobody may connect between the socket appearing and its owner being set.
	old := syscall.Umask(0o177)
	listener, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Chown(path, *uid, -1); err != nil {
		log.Fatal(err)
	}

	sup := &helper.Supervisor{CorePath: *corePath, Guard: helper.NewGuard()}
	if sup.Guard != nil {
		// The guard tells the core from everyone else by its group.
		sup.CoreGID = helper.CoreGID
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		sup.Stop()
		listener.Close()
		_ = os.Remove(path)
		os.Exit(0)
	}()
	log.Printf("listening on %s for uid %d", path, *uid)
	log.Fatal(sup.Serve(listener))
}
