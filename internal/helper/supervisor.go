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
	"syscall"
	"time"

	"github.com/oviron/flclash-desktop/internal/compile"
	"github.com/oviron/flclash-desktop/internal/ingest"
	"github.com/oviron/flclash-desktop/internal/policy"
)

// tunPrefix is the address range of the tunnel device the core creates.
var tunPrefix = netip.MustParsePrefix("198.18.0.0/30")

// Supervisor runs at most one core process and answers requests about it.
type Supervisor struct {
	// CorePath is the core binary; it reads frames on stdin and answers on a
	// descriptor of its own.
	CorePath string
	// Captured reports whether the tunnel device exists; replaced in tests.
	Captured func() bool
	// Resolvers reads the resolvers of the network the host is on; replaced in tests.
	Resolvers func() []string
	// DeviceTimeout is how long to wait for the device after the core accepted
	// a config; zero means eight seconds.
	DeviceTimeout time.Duration
	// Guard keeps traffic from leaving past the tunnel; nil where there is none.
	Guard Guard
	// CoreGID, when not zero, is the group the core runs under, so that the
	// guard can tell its connections from all others.
	CoreGID int
	// TunName finds the tunnel device; replaced in tests.
	TunName func() string
	// RecoverAfter are the waits before each try to bring back a core that
	// died under a full guard; nil means the default.
	RecoverAfter []time.Duration

	mu      sync.Mutex
	core    *exec.Cmd
	toCore  io.WriteCloser
	replies *bufio.Reader
	exited  chan struct{}
	status  Status
	// directDNS is what Resolvers gave when the core was started. It is read
	// only then: with the tunnel up the system resolver is the core itself.
	directDNS []string
	// last is the request that runs, or ran when the core died.
	last Request
	// orders counts what the user asked for; a recovery started before the
	// latest order gives up.
	orders int
	// guardProven: the full guard was seen to let the core through.
	guardProven bool
}

// Serve answers requests until the listener is closed.
func (s *Supervisor) Serve(l net.Listener) error {
	s.init()
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Supervisor) init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Captured == nil {
		s.Captured = tunPresent
	}
	if s.Resolvers == nil {
		s.Resolvers = systemResolvers
	}
	if s.TunName == nil {
		s.TunName = tunName
	}
	if s.Guard == nil {
		return
	}
	// Rules found at start were left by a helper that died with a tunnel up.
	// A full guard stays: the person turned it on and nothing took it off.
	if s.Guard.Holding() {
		log.Printf("a full guard is in place from before: staying blocked until told otherwise")
		s.status = Status{Blocked: true, Guard: GuardFull}
	} else if err := s.Guard.Release(); err != nil {
		log.Printf("guard: %v", err)
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

// Status is the current state.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Captured = st.Running && s.Captured()
	st.Version = Version
	return st
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
	s.orders++
	return s.run(req)
}

// run starts the core with the request, or moves a running core to it.
// Caller holds mu.
func (s *Supervisor) run(req Request) error {
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
			s.giveUp()
		}
		// A running core that refused the config keeps the one it had.
		return err
	}

	// The core reports success before the device exists; wait for the device
	// itself, since only it means anything is captured.
	wait := s.DeviceTimeout
	if wait == 0 {
		wait = 8 * time.Second
	}
	deadline := time.Now().Add(wait)
	for !s.Captured() {
		if time.Now().After(deadline) {
			s.giveUp()
			return errors.New("the core started but the tunnel device did not appear; see the helper log")
		}
		time.Sleep(100 * time.Millisecond)
	}

	since := s.status.Since
	if fresh {
		since = time.Now()
	}
	s.status = Status{Running: true, Server: req.Server.Name, Policy: req.Policy, Revision: req.Revision, Since: since}
	s.last = req
	// The guard goes on before anyone is told the tunnel is up.
	s.engage(req)
	return nil
}

// giveUp ends a core that did not come up. A full guard that was holding
// stays: a failed try to reconnect must not open what was closed.
func (s *Supervisor) giveUp() {
	s.halt()
	if s.status.Blocked {
		s.status = Status{Blocked: true, Guard: GuardFull, Server: s.status.Server, Policy: s.status.Policy}
		return
	}
	s.release()
	s.status = Status{}
}

// engage puts the guard on for what runs now. A guard that does not go on
// does not stop the tunnel: the status says what protection there is.
// Caller holds mu.
func (s *Supervisor) engage(req Request) {
	if s.Guard == nil {
		return
	}
	tun := s.TunName()
	if err := s.Guard.Engage(tun, s.CoreGID, req.Protect); err != nil {
		log.Printf("guard: %v", err)
		s.status.GuardProblem = err.Error()
		return
	}
	s.status.Guard = GuardIPv6
	if !req.Protect {
		return
	}
	s.status.Guard = GuardFull
	if s.guardProven {
		return
	}
	// The full guard refuses everything but the core. If it refused the core
	// as well, the tunnel would be up and dead; ask the core to reach the
	// server and see.
	if _, err := s.measure(req.Server); err == nil {
		s.guardProven = true
		return
	}
	_ = s.Guard.Engage(tun, s.CoreGID, false)
	if _, err := s.measure(req.Server); err != nil {
		// The server does not answer either way: the guard is not the cause.
		if err := s.Guard.Engage(tun, s.CoreGID, true); err != nil {
			s.status.Guard, s.status.GuardProblem = GuardIPv6, err.Error()
		}
		return
	}
	log.Printf("guard: the full rules cut the core off from the server; left IPv6 only")
	s.status.Guard = GuardIPv6
	s.status.GuardProblem = "the firewall rules cut the core off from the server, so traffic past the tunnel is not blocked"
}

// release takes the guard off. Caller holds mu.
func (s *Supervisor) release() {
	if s.Guard == nil {
		return
	}
	if err := s.Guard.Release(); err != nil {
		log.Printf("guard: %v", err)
	}
}

// start launches the core. Caller holds mu.
func (s *Supervisor) start() error {
	cmd := exec.Command(s.CorePath)
	if s.CoreGID != 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 0, Gid: uint32(s.CoreGID), NoSetGroups: true}}
	}
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
		s.coreExited(cmd)
	}()
	s.core, s.toCore, s.replies, s.exited = cmd, stdin, bufio.NewReader(replies), exited
	return nil
}

// coreExited is told of every end of a core. One that the supervisor ended
// itself is no longer its core by then; what is left is a core that died.
func (s *Supervisor) coreExited(cmd *exec.Cmd) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.core != cmd {
		return
	}
	s.core, s.toCore, s.replies = nil, nil, nil
	if s.Guard == nil || s.status.Guard != GuardFull {
		// Nothing was promised: the tunnel is off, and so is what came with it.
		s.release()
		s.status = Status{}
		return
	}
	// The device is gone and its name may go to someone else: keep the rules
	// without it. Nothing leaves until the core is back or the person says so.
	if err := s.Guard.Engage("", s.CoreGID, true); err != nil {
		log.Printf("guard: %v", err)
	}
	log.Printf("the core died under a full guard: blocked, trying to bring it back")
	s.status = Status{Blocked: true, Guard: GuardFull, Server: s.last.Server.Name, Policy: s.last.Policy}
	go s.recover(s.orders)
}

// recover tries to bring the core back with what it ran. It stops at the
// first order from the user: from then on the user decides.
func (s *Supervisor) recover(orders int) {
	waits := s.RecoverAfter
	if waits == nil {
		waits = []time.Duration{time.Second, 3 * time.Second, 10 * time.Second}
	}
	for _, wait := range waits {
		time.Sleep(wait)
		s.mu.Lock()
		if s.orders != orders || !s.status.Blocked || s.core != nil {
			s.mu.Unlock()
			return
		}
		err := s.run(s.last)
		s.mu.Unlock()
		if err == nil {
			log.Printf("the core is back")
			return
		}
		log.Printf("the core did not come back: %v", err)
	}
}

// delay measures a server from inside the running core, where connections
// leave past the tunnel the way the core's own do.
func (s *Supervisor) delay(req Request) (int, error) {
	if err := ingest.Check(req.Server); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.core == nil {
		return 0, errors.New("the tunnel is off: there is no core to measure from")
	}
	return s.measure(req.Server)
}

// measure asks the core for the delay of a server. Caller holds mu.
func (s *Supervisor) measure(server ingest.Server) (int, error) {
	body, err := json.Marshal(CoreDelay{
		Proxy:     compile.Proxy(server),
		URL:       DelayURL,
		TimeoutMS: int(DelayTimeout / time.Millisecond),
	})
	if err != nil {
		return 0, err
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

// Stop ends the core; the tunnel and its routes go with the process, and the
// guard comes off last.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orders++
	s.halt()
	s.release()
	s.status = Status{}
}

// halt ends the core process and nothing else. Caller holds mu.
func (s *Supervisor) halt() {
	cmd, toCore, exited := s.core, s.toCore, s.exited
	if cmd == nil {
		return
	}
	// From here on its end is expected: see coreExited.
	s.core, s.toCore, s.replies = nil, nil, nil
	// Closing stdin asks the core to shut down in order.
	_ = toCore.Close()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-exited
	}
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
