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
	"strings"
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
	// Resolvers reads the resolvers of the network the host is on; replaced in tests.
	Resolvers func() []string
	// DeviceTimeout is how long to wait for the device after the core accepted
	// a config; zero means eight seconds.
	DeviceTimeout time.Duration

	mu      sync.Mutex
	core    *exec.Cmd
	toCore  io.WriteCloser
	replies *bufio.Reader
	exited  chan struct{}
	status  Status
	// directDNS is what Resolvers gave when the core was started. It is read
	// only then: with the tunnel up the system resolver is the core itself.
	directDNS []string
}

// Serve answers requests until the listener is closed.
func (s *Supervisor) Serve(l net.Listener) error {
	if s.Captured == nil {
		s.Captured = tunPresent
	}
	if s.Resolvers == nil {
		s.Resolvers = systemResolvers
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
	var err error
	if req.Op == OpDelay {
		resp.Delay, err = s.delay(req)
	} else {
		err = s.do(req)
	}
	if err != nil {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reap()
	if s.core != nil && req.Revision < s.status.Revision {
		return errors.New("a newer choice is already applied")
	}
	fresh := s.core == nil
	if fresh && s.Resolvers != nil {
		s.directDNS = s.Resolvers()
	}
	config, err := compile.Config(compile.Input{
		Server:    req.Server,
		Policy:    policy.Policy{Name: req.Policy, Rules: req.Rules},
		TUN:       true,
		OS:        runtime.GOOS,
		DirectDNS: s.directDNS,
	})
	if err != nil {
		return err
	}
	if fresh {
		if err := s.start(); err != nil {
			return err
		}
	}
	if _, err := s.send(FrameConfig, config); err != nil {
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

// delay measures a server from inside the running core, where connections
// leave past the tunnel the way the core's own do.
func (s *Supervisor) delay(req Request) (int, error) {
	if err := ingest.Check(req.Server); err != nil {
		return 0, err
	}
	body, err := json.Marshal(CoreDelay{
		Proxy:     compile.Proxy(req.Server),
		URL:       DelayURL,
		TimeoutMS: int(DelayTimeout / time.Millisecond),
	})
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reap()
	if s.core == nil {
		return 0, errors.New("the tunnel is off: there is no core to measure from")
	}
	reply, err := s.send(FrameDelay, body)
	return reply.Delay, err
}

// send gives the core a frame and waits for its answer. Caller holds mu.
func (s *Supervisor) send(kind byte, body []byte) (CoreReply, error) {
	var reply CoreReply
	frame := make([]byte, 5, 5+len(body))
	binary.BigEndian.PutUint32(frame, uint32(len(body)+1))
	frame[4] = kind
	if _, err := s.toCore.Write(append(frame, body...)); err != nil {
		return reply, fmt.Errorf("the core is gone: %w", err)
	}
	type answer struct {
		line string
		err  error
	}
	got := make(chan answer, 1)
	replies := s.replies
	go func() {
		line, err := replies.ReadString('\n')
		got <- answer{line, err}
	}()
	select {
	case v := <-got:
		if v.err != nil {
			return reply, fmt.Errorf("the core is gone: %w", v.err)
		}
		if err := json.Unmarshal([]byte(v.line), &reply); err != nil {
			return reply, fmt.Errorf("the core answered nonsense: %w", err)
		}
		if reply.Error != "" {
			return reply, errors.New(reply.Error)
		}
		return reply, nil
	case <-time.After(60 * time.Second):
		return reply, errors.New("the core did not answer in a minute")
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

// systemResolvers reads the resolvers the system uses while no tunnel is up.
// The tunnel's own address range is left out: it would be the core asking itself.
func systemResolvers() []string {
	raw, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	return parseResolvers(string(raw))
}

func parseResolvers(text string) []string {
	fakeRange := netip.MustParsePrefix("198.18.0.0/15")
	var out []string
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		host, _, _ := strings.Cut(fields[1], "%")
		addr, err := netip.ParseAddr(host)
		// A link-local address is of no use without the interface it was cut from.
		if err != nil || fakeRange.Contains(addr) || addr.IsUnspecified() || addr.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, addr.String())
	}
	return out
}
