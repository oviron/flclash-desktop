package helper

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/oviron/flclash-desktop/internal/compile"
	"github.com/oviron/flclash-desktop/internal/ingest"
	"github.com/oviron/flclash-desktop/internal/policy"
)

// tunPrefix is the address range of the tunnel device the core creates.
var tunPrefix = netip.MustParsePrefix("198.18.0.0/30")

// Supervisor runs at most one core process and answers requests about it.
type Supervisor struct {
	// CorePath is the core binary; it reads configs on stdin and answers on stdout.
	CorePath string
	// Captured reports whether the tunnel device exists; replaced in tests.
	Captured func() bool
	// DeviceTimeout is how long to wait for the device after the core accepted
	// a config; zero means eight seconds.
	DeviceTimeout time.Duration

	mu      sync.Mutex
	core    *exec.Cmd
	toCore  io.WriteCloser
	replies *bufio.Reader
	exited  chan struct{}
	status  Status
}

// Serve answers requests until the listener is closed.
func (s *Supervisor) Serve(l net.Listener) error {
	if s.Captured == nil {
		s.Captured = tunPresent
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Supervisor) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&req); err != nil {
		return
	}
	var resp Response
	if err := s.do(req); err != nil {
		resp.Error = err.Error()
	}
	resp.Status = s.Status()
	_ = json.NewEncoder(conn).Encode(resp)
}

func (s *Supervisor) do(req Request) error {
	switch req.Op {
	case OpStatus:
		return nil
	case OpStop:
		s.Stop()
		return nil
	case OpApply:
		return s.apply(req)
	}
	return fmt.Errorf("unknown operation %q", req.Op)
}

// Status is the current state, with a core that died noticed.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reap()
	st := s.status
	st.Captured = st.Running && s.Captured()
	st.Version = Version
	return st
}

// reap clears the state when the core is gone. Caller holds mu.
func (s *Supervisor) reap() {
	if s.core == nil {
		return
	}
	select {
	case <-s.exited:
		s.core, s.toCore, s.replies = nil, nil, nil
		s.status = Status{}
	default:
	}
}

func (s *Supervisor) apply(req Request) error {
	if err := ingest.Check(req.Server); err != nil {
		return err
	}
	if err := policy.Check(req.Rules); err != nil {
		return fmt.Errorf("the routing policy was refused: %w", err)
	}
	config, err := compile.Config(compile.Input{
		Server: req.Server,
		Policy: policy.Policy{Name: req.Policy, Rules: req.Rules},
		TUN:    true,
		OS:     runtime.GOOS,
	})
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.reap()
	if s.core != nil && req.Revision < s.status.Revision {
		return errors.New("a newer choice is already applied")
	}
	fresh := s.core == nil
	if fresh {
		if err := s.start(); err != nil {
			return err
		}
	}
	if err := s.send(config); err != nil {
		if fresh {
			s.stop()
		}
		// A running core that refused the config keeps the one it had.
		return err
	}
	if fresh {
		s.status.Since = time.Now()
	}
	s.status.Running = true
	s.status.Server, s.status.Policy, s.status.Revision = req.Server.Name, req.Policy, req.Revision

	// The core reports success before the device exists; wait for the device
	// itself, since only it means anything is captured.
	wait := s.DeviceTimeout
	if wait == 0 {
		wait = 8 * time.Second
	}
	deadline := time.Now().Add(wait)
	for !s.Captured() {
		if time.Now().After(deadline) {
			s.stop()
			return errors.New("the core started but the tunnel device did not appear; see the helper log")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// start launches the core. Caller holds mu.
func (s *Supervisor) start() error {
	cmd := exec.Command(s.CorePath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	// The core's own log goes to stdout and stderr; its verdicts come on a
	// pipe of their own, which the child sees as descriptor 3.
	replies, verdicts, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.ExtraFiles = []*os.File{verdicts}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	err = cmd.Start()
	verdicts.Close()
	if err != nil {
		replies.Close()
		return fmt.Errorf("the core did not start: %w", err)
	}
	exited := make(chan struct{})
	go func() {
		err := cmd.Wait()
		log.Printf("core exited: %v", err)
		replies.Close()
		close(exited)
	}()
	s.core, s.toCore, s.replies, s.exited = cmd, stdin, bufio.NewReader(replies), exited
	return nil
}

// send gives the core a config and waits for its verdict. Caller holds mu.
func (s *Supervisor) send(config []byte) error {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(config)))
	if _, err := s.toCore.Write(append(size[:], config...)); err != nil {
		return fmt.Errorf("the core is gone: %w", err)
	}
	type verdict struct {
		line string
		err  error
	}
	got := make(chan verdict, 1)
	replies := s.replies
	go func() {
		line, err := replies.ReadString('\n')
		got <- verdict{line, err}
	}()
	select {
	case v := <-got:
		if v.err != nil {
			return fmt.Errorf("the core is gone: %w", v.err)
		}
		var reply struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(v.line), &reply); err != nil {
			return fmt.Errorf("the core answered nonsense: %w", err)
		}
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		return nil
	case <-time.After(60 * time.Second):
		return errors.New("the core did not answer in a minute")
	}
}

// Stop ends the core; the tunnel and its routes go with the process.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reap()
	s.stop()
}

// stop is Stop for a caller that holds mu.
func (s *Supervisor) stop() {
	if s.core == nil {
		return
	}
	// Closing stdin asks the core to shut down in order.
	_ = s.toCore.Close()
	select {
	case <-s.exited:
	case <-time.After(5 * time.Second):
		_ = s.core.Process.Kill()
		<-s.exited
	}
	s.core, s.toCore, s.replies = nil, nil, nil
	s.status = Status{}
}

func tunPresent() bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if addr, ok := netip.AddrFromSlice(ipnet.IP); ok && tunPrefix.Contains(addr.Unmap()) {
				return true
			}
		}
	}
	return false
}
