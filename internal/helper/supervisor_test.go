package helper

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oviron/flclash-desktop/internal/ingest"
)

// The test binary doubles as the core: with FAKE_CORE set it speaks the core's
// side of the protocol and refuses any config that names the server "refuse-me".
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_CORE") == "" {
		os.Exit(m.Run())
	}
	// The answers go where -verdicts says, as with the real core.
	verdicts := uintptr(3)
	for i, arg := range os.Args {
		if arg == "-verdicts" && i+1 < len(os.Args) {
			n, _ := strconv.ParseUint(os.Args[i+1], 10, 64)
			verdicts = uintptr(n)
		}
	}
	out := json.NewEncoder(os.NewFile(verdicts, "verdicts"))
	delays := 0
	for {
		var size [4]byte
		if _, err := io.ReadFull(os.Stdin, size[:]); err != nil {
			return
		}
		frame := make([]byte, binary.BigEndian.Uint32(size[:]))
		if _, err := io.ReadFull(os.Stdin, frame); err != nil {
			return
		}
		var reply CoreReply
		if bytes.Contains(frame, []byte("refuse-me")) {
			reply.Error = "the core refused the config"
		}
		if bytes.Contains(frame, []byte("die-now")) {
			os.Exit(3)
		}
		if frame[0] == FrameExit {
			// The address is asked through the server that runs, or the one
			// the request names; each has an exit of its own.
			if bytes.Contains(frame, []byte(`"name":"first"`)) && bytes.Contains(frame, []byte(ExitURL)) {
				reply.Exit = &Exit{IP: "203.0.113.7", Country: "NL"}
			} else if bytes.Contains(frame, []byte(`"name":"other"`)) && bytes.Contains(frame, []byte(ExitURL)) {
				reply.Exit = &Exit{IP: "198.51.100.9", Country: "DE"}
			} else {
				reply.Error = "asked through the wrong server"
			}
		}
		if frame[0] == FrameStats {
			reply.Stats = &Stats{Down: 1000, Open: 1, Connections: []Connection{{ID: "a", Host: "example.org", Port: 443, Via: "first"}}}
		}
		if frame[0] == FrameDelay && reply.Error == "" {
			delays++
			switch {
			case bytes.Contains(frame, []byte("no-answer")):
				reply.Error = "no answer"
			case bytes.Contains(frame, []byte("cut-by-guard")) && delays == 1:
				// The first try fails, the next one, made with the guard
				// loosened, goes through.
				reply.Error = "no answer"
			default:
				reply.Delay = 42
			}
		}
		_ = out.Encode(reply)
	}
}

func supervisor(t *testing.T, captured bool) *Supervisor {
	t.Helper()
	t.Setenv("FAKE_CORE", "1")
	s := &Supervisor{
		CorePath:      os.Args[0],
		Captured:      func() bool { return captured },
		DeviceTimeout: 300 * time.Millisecond,
	}
	t.Cleanup(s.Stop)
	return s
}

func request(name string, revision int64) Request {
	return Request{
		Op:       OpApply,
		Mode:     ModeTUN,
		Revision: revision,
		Server:   ingest.Server{Name: name, Proxy: map[string]any{"type": "hysteria2", "server": "a.example", "port": 443, "password": "pw"}},
		Policy:   "all",
		Rules:    []string{"MATCH,PROXY"},
	}
}

func TestApplySwitchStop(t *testing.T) {
	s := supervisor(t, true)
	if err := s.do(request("first", 10)); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if !st.Running || !st.Captured || st.Server != "first" || st.Revision != 10 {
		t.Fatalf("got %+v", st)
	}
	pid, since := s.core.Process.Pid, st.Since

	// A switch goes to the same core: the tunnel must not drop in between.
	if err := s.do(request("second", 11)); err != nil {
		t.Fatal(err)
	}
	if st = s.Status(); st.Server != "second" || s.core.Process.Pid != pid || !st.Since.Equal(since) {
		t.Fatalf("a switch restarted the core: %+v", st)
	}

	// A request older than what runs is a late one and changes nothing.
	if err := s.do(request("late", 5)); err == nil || !strings.Contains(err.Error(), "newer choice") {
		t.Fatalf("got %v", err)
	}
	if st = s.Status(); st.Server != "second" {
		t.Fatalf("a late request took effect: %+v", st)
	}

	if err := s.do(Request{Op: OpStop}); err != nil {
		t.Fatal(err)
	}
	if st = s.Status(); st.Running || st.Captured || st.Server != "" {
		t.Fatalf("got %+v after stop", st)
	}
}

func TestARefusedConfigKeepsWhatRuns(t *testing.T) {
	s := supervisor(t, true)
	if err := s.do(request("refuse-me", 1)); err == nil {
		t.Fatal("the first config was refused by the core, apply must fail")
	}
	if st := s.Status(); st.Running {
		t.Fatalf("nothing should run after a refused first config: %+v", st)
	}
	if err := s.do(request("good", 2)); err != nil {
		t.Fatal(err)
	}
	if err := s.do(request("refuse-me", 3)); err == nil {
		t.Fatal("apply must report the refusal")
	}
	if st := s.Status(); !st.Running || st.Server != "good" || st.Revision != 2 {
		t.Fatalf("the working config was lost: %+v", st)
	}
}

func TestNoDeviceMeansNotConnected(t *testing.T) {
	s := supervisor(t, false)
	err := s.do(request("first", 1))
	if err == nil || !strings.Contains(err.Error(), "device did not appear") {
		t.Fatalf("got %v", err)
	}
	if st := s.Status(); st.Running || st.Captured {
		t.Fatalf("a core without a tunnel must not be left running: %+v", st)
	}
}

func TestBadInputNeverReachesTheCore(t *testing.T) {
	s := supervisor(t, true)
	bad := request("first", 1)
	bad.Server.Proxy["private-key"] = "/etc/master.passwd"
	if err := s.do(bad); err == nil {
		t.Fatal("a server with an unknown field must be refused")
	}
	bad = request("first", 1)
	bad.Rules = []string{"SCRIPT,x,PROXY", "MATCH,PROXY"}
	if err := s.do(bad); err == nil {
		t.Fatal("rules outside the policy language must be refused")
	}
	if s.core != nil {
		t.Fatal("the core was started for a refused request")
	}
	if err := s.do(Request{Op: "reboot"}); err == nil {
		t.Fatal("an unknown operation must be refused")
	}
}

func TestADeadCoreIsNoticed(t *testing.T) {
	s := supervisor(t, true)
	if err := s.do(request("first", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.do(request("die-now", 2)); err == nil {
		t.Fatal("apply to a core that died must fail")
	}
	deadline := time.Now().Add(3 * time.Second)
	for s.Status().Running {
		if time.Now().After(deadline) {
			t.Fatal("a dead core is still reported as running")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestResolversFromResolvConf(t *testing.T) {
	got := parseResolvers("# a comment\nsearch lan\nnameserver 192.168.1.1\nnameserver 198.18.0.2\nnameserver fe80::1%en0\nnameserver 0.0.0.0\nnameserver nonsense\n")
	want := []string{"192.168.1.1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDelay(t *testing.T) {
	s := supervisor(t, true)
	measure := Request{Op: OpDelay, Server: request("other", 0).Server}
	if _, err := s.delay(measure); err == nil {
		t.Fatal("with no core there is nothing to measure from")
	}
	if err := s.do(request("first", 10)); err != nil {
		t.Fatal(err)
	}
	ms, err := s.delay(measure)
	if err != nil || ms != 42 {
		t.Fatalf("got %d, %v", ms, err)
	}
	// Measuring another server leaves the running one as it was.
	if st := s.Status(); !st.Running || st.Server != "first" {
		t.Fatalf("got %#v", st)
	}
	// A server the helper would not run is not measured either.
	bad := measure
	bad.Server.Proxy = map[string]any{"type": "http", "server": "a.example", "port": 80}
	if _, err := s.delay(bad); err == nil {
		t.Fatal("a server of a kind that is not allowed must be refused")
	}
}

// fakeGuard remembers what was asked of it.
type fakeGuard struct {
	mu   sync.Mutex
	on   bool
	full bool
	tun  string
	log  []string
}

func (g *fakeGuard) Engage(tun string, _ int, full bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.on, g.full, g.tun = true, full, tun
	g.log = append(g.log, fmt.Sprintf("engage tun=%q full=%v", tun, full))
	return nil
}

func (g *fakeGuard) Release() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.on, g.full = false, false
	g.log = append(g.log, "release")
	return nil
}

func (g *fakeGuard) Holding() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.on && g.full
}

func guarded(t *testing.T) (*Supervisor, *fakeGuard) {
	t.Helper()
	s := supervisor(t, true)
	g := &fakeGuard{}
	s.Guard = g
	s.TunName = func() string { return "utun9" }
	s.RecoverAfter = []time.Duration{20 * time.Millisecond}
	return s, g
}

func protected(name string, revision int64) Request {
	req := request(name, revision)
	req.Protect = true
	return req
}

// kill makes the fake core die the way a crash would.
func kill(s *Supervisor) {
	_, _ = s.delay(Request{Op: OpDelay, Server: request("die-now", 0).Server})
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("did not happen: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestGuardFollowsTheTunnel(t *testing.T) {
	s, g := guarded(t)
	if err := s.do(request("first", 1)); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Guard != GuardIPv6 || g.full || g.tun != "utun9" {
		t.Fatalf("without protect only IPv6 is closed: %+v, %v", st, g.log)
	}
	if err := s.do(protected("first", 2)); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Guard != GuardFull || st.GuardProblem != "" || !g.Holding() {
		t.Fatalf("got %+v, %v", st, g.log)
	}
	s.Stop()
	if st := s.Status(); st.Guard != "" || g.on {
		t.Fatalf("the guard must come off with the tunnel: %+v, %v", st, g.log)
	}
}

func TestACoreThatDiesUnderAFullGuardLeavesTheHostBlocked(t *testing.T) {
	s, g := guarded(t)
	// No recovery in this test: see what is true while there is none.
	s.RecoverAfter = []time.Duration{}
	if err := s.do(protected("first", 1)); err != nil {
		t.Fatal(err)
	}
	kill(s)
	waitFor(t, "blocked", func() bool { return s.Status().Blocked })
	st := s.Status()
	if st.Running || st.Captured || st.Server != "first" || !g.Holding() || g.tun != "" {
		t.Fatalf("got %+v, %v", st, g.log)
	}
	// Turning it off is what opens the host again.
	s.Stop()
	if st := s.Status(); st.Blocked || g.on {
		t.Fatalf("got %+v, %v", st, g.log)
	}
}

func TestACoreThatDiesUnderAFullGuardIsBroughtBack(t *testing.T) {
	s, g := guarded(t)
	if err := s.do(protected("first", 1)); err != nil {
		t.Fatal(err)
	}
	kill(s)
	waitFor(t, "the core is back", func() bool { st := s.Status(); return st.Running && !st.Blocked })
	if st := s.Status(); st.Server != "first" || st.Guard != GuardFull || !g.Holding() || g.tun != "utun9" {
		t.Fatalf("got %+v, %v", st, g.log)
	}
	// The guard never came off in between.
	for _, entry := range g.log {
		if entry == "release" {
			t.Fatalf("the guard was released on the way: %v", g.log)
		}
	}
}

func TestACoreThatDiesWithoutProtectTurnsEverythingOff(t *testing.T) {
	s, g := guarded(t)
	if err := s.do(request("first", 1)); err != nil {
		t.Fatal(err)
	}
	kill(s)
	waitFor(t, "off", func() bool { return !s.Status().Running })
	time.Sleep(60 * time.Millisecond) // a recovery, if one was started, would have run
	if st := s.Status(); st.Blocked || st.Running || g.on {
		t.Fatalf("got %+v, %v", st, g.log)
	}
}

func TestAGuardThatCutsTheCoreOffIsLoosened(t *testing.T) {
	s, g := guarded(t)
	if err := s.do(protected("cut-by-guard", 1)); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if !st.Running || st.Guard != GuardIPv6 || st.GuardProblem == "" || g.full {
		t.Fatalf("got %+v, %v", st, g.log)
	}
}

func TestADeadServerIsNotBlamedOnTheGuard(t *testing.T) {
	s, g := guarded(t)
	if err := s.do(protected("no-answer", 1)); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Guard != GuardFull || st.GuardProblem != "" || !g.Holding() {
		t.Fatalf("got %+v, %v", st, g.log)
	}
}

func TestAGuardLeftByADeadHelperKeepsTheHostBlocked(t *testing.T) {
	s, g := guarded(t)
	g.on, g.full = true, true
	s.init()
	if st := s.Status(); !st.Blocked || st.Running {
		t.Fatalf("got %+v", st)
	}
	if err := s.do(protected("first", 1)); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Blocked || !st.Running {
		t.Fatalf("connecting must end the block: %+v", st)
	}
}

// fakeProxy is the system proxy as the tests see it.
type fakeProxy struct {
	mu   sync.Mutex
	port int
	log  []string
}

func (p *fakeProxy) Set(port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.port = port
	p.log = append(p.log, fmt.Sprintf("set %d", port))
	return nil
}

func (p *fakeProxy) Clear() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.port != 0 {
		p.log = append(p.log, "clear")
	}
	p.port = 0
	return nil
}

func (p *fakeProxy) current() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.port
}

func withModes(t *testing.T) (*Supervisor, *fakeGuard, *fakeProxy) {
	t.Helper()
	s, g := guarded(t)
	p := &fakeProxy{}
	s.SystemProxy = p
	s.HomeDir = t.TempDir()
	s.Listening = func(int) bool { return true }
	s.PortFree = func(int) bool { return true }
	return s, g, p
}

func inMode(mode string, port int, revision int64) Request {
	req := protected("first", revision)
	req.Mode, req.Port = mode, port
	return req
}

func TestTheProxyModesTouchOnlyWhatTheyPromise(t *testing.T) {
	s, g, p := withModes(t)
	if err := s.do(inMode(ModeProxy, 7890, 1)); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if !st.Running || !st.Captured || st.Mode != ModeProxy || st.Port != 7890 || st.Guard != "" || g.on || p.current() != 0 {
		t.Fatalf("a plain proxy changes nothing in the system: %+v, %v, %v", st, g.log, p.log)
	}
	pid := s.core.Process.Pid

	// The same core serves the system mode: only the system setting changes.
	if err := s.do(inMode(ModeSystem, 7890, 2)); err != nil {
		t.Fatal(err)
	}
	if st = s.Status(); st.Mode != ModeSystem || p.current() != 7890 || s.core.Process.Pid != pid {
		t.Fatalf("got %+v, %v", st, p.log)
	}
	if err := s.do(inMode(ModeProxy, 7890, 3)); err != nil {
		t.Fatal(err)
	}
	if p.current() != 0 || s.core.Process.Pid != pid {
		t.Fatalf("leaving the system mode must put the system proxy back: %v", p.log)
	}

	// The tunnel needs another core, and brings the guard.
	if err := s.do(inMode(ModeTUN, 7890, 4)); err != nil {
		t.Fatal(err)
	}
	if st = s.Status(); st.Mode != ModeTUN || st.Guard != GuardFull || s.core.Process.Pid == pid {
		t.Fatalf("got %+v, %v", st, g.log)
	}
	// And back: the guard comes off with the tunnel.
	if err := s.do(inMode(ModeSystem, 7890, 5)); err != nil {
		t.Fatal(err)
	}
	if st = s.Status(); st.Mode != ModeSystem || st.Guard != "" || g.on || p.current() != 7890 {
		t.Fatalf("got %+v, %v, %v", st, g.log, p.log)
	}
	s.Stop()
	if p.current() != 0 {
		t.Fatalf("stopping must put the system proxy back: %v", p.log)
	}
}

func TestACoreThatDiesInTheSystemModeWithoutProtectFreesTheSystem(t *testing.T) {
	s, _, p := withModes(t)
	req := inMode(ModeSystem, 7890, 1)
	req.Protect = false
	if err := s.do(req); err != nil {
		t.Fatal(err)
	}
	kill(s)
	waitFor(t, "off", func() bool { return !s.Status().Running })
	time.Sleep(60 * time.Millisecond) // a recovery, if one was started, would have run
	if st := s.Status(); st.Blocked || st.Running || p.current() != 0 {
		t.Fatalf("the system must not be left pointing at a dead port: %+v, %v", st, p.log)
	}
}

func TestACoreThatDiesInTheSystemModeUnderProtectKeepsTheSystemProxy(t *testing.T) {
	s, _, p := withModes(t)
	s.RecoverAfter = []time.Duration{}
	if err := s.do(inMode(ModeSystem, 7890, 1)); err != nil {
		t.Fatal(err)
	}
	kill(s)
	waitFor(t, "blocked", func() bool { return s.Status().Blocked })
	if st := s.Status(); st.Running || st.Mode != ModeSystem || p.current() != 7890 {
		t.Fatalf("programs must stop, not go out directly: %+v, %v", st, p.log)
	}

	// A helper that starts anew finds the mark and stays blocked.
	again := &Supervisor{CorePath: s.CorePath, Captured: s.Captured, SystemProxy: p, HomeDir: s.HomeDir}
	again.init()
	if st := again.Status(); !st.Blocked || st.Mode != ModeSystem || p.current() != 7890 {
		t.Fatalf("got %+v, %v", st, p.log)
	}

	s.Stop()
	if st := s.Status(); st.Blocked || p.current() != 0 {
		t.Fatalf("turning it off frees the system: %+v, %v", st, p.log)
	}
	fresh := &Supervisor{CorePath: s.CorePath, Captured: s.Captured, SystemProxy: p, HomeDir: s.HomeDir}
	fresh.init()
	if st := fresh.Status(); st.Blocked {
		t.Fatalf("nothing is kept after it was turned off: %+v", st)
	}
}

func TestACoreThatDiesInTheSystemModeUnderProtectIsBroughtBack(t *testing.T) {
	s, _, p := withModes(t)
	if err := s.do(inMode(ModeSystem, 7890, 1)); err != nil {
		t.Fatal(err)
	}
	kill(s)
	waitFor(t, "the core is back", func() bool { st := s.Status(); return st.Running && !st.Blocked })
	if st := s.Status(); st.Mode != ModeSystem || p.current() != 7890 {
		t.Fatalf("got %+v, %v", st, p.log)
	}
	for _, entry := range p.log {
		if entry == "clear" {
			t.Fatalf("the system proxy was cleared on the way: %v", p.log)
		}
	}
}

func TestLeavingABlockedTunnelForAProxyModeTakesTheGuardOff(t *testing.T) {
	s, g, p := withModes(t)
	s.RecoverAfter = []time.Duration{}
	if err := s.do(inMode(ModeTUN, 7890, 1)); err != nil {
		t.Fatal(err)
	}
	kill(s)
	waitFor(t, "blocked", func() bool { return s.Status().Blocked })
	if err := s.do(inMode(ModeSystem, 7890, 2)); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); !st.Running || st.Blocked || st.Guard != "" || g.on || p.current() != 7890 {
		t.Fatalf("got %+v, %v, %v", st, g.log, p.log)
	}
}

func TestTheLocalPortIsChecked(t *testing.T) {
	s, _, _ := withModes(t)
	for _, req := range []Request{inMode(ModeProxy, 0, 1), inMode(ModeSystem, 80, 1), inMode(ModeTUN, 70000, 1), inMode("else", 7890, 1), inMode("", 7890, 1)} {
		if err := s.do(req); err == nil {
			t.Errorf("mode %q with port %d must be refused", req.Mode, req.Port)
		}
	}
	s.PortFree = func(int) bool { return false }
	err := s.do(inMode(ModeProxy, 7890, 2))
	if err == nil || !strings.Contains(err.Error(), "taken") {
		t.Fatalf("got %v", err)
	}
	if st := s.Status(); st.Running {
		t.Fatalf("got %+v", st)
	}
}

func TestAPortThatDoesNotOpenMeansNotConnected(t *testing.T) {
	s, _, p := withModes(t)
	s.Listening = func(int) bool { return false }
	err := s.do(inMode(ModeSystem, 7890, 1))
	if err == nil || !strings.Contains(err.Error(), "port did not open") {
		t.Fatalf("got %v", err)
	}
	if st := s.Status(); st.Running || p.current() != 0 {
		t.Fatalf("got %+v, %v", st, p.log)
	}
}

func TestStats(t *testing.T) {
	s := supervisor(t, true)
	if _, err := s.stats(); err == nil {
		t.Fatal("with nothing running there are no numbers")
	}
	if err := s.do(request("first", 1)); err != nil {
		t.Fatal(err)
	}
	stats, err := s.stats()
	if err != nil || stats.Down != 1000 || len(stats.Connections) != 1 || stats.Connections[0].Host != "example.org" {
		t.Fatalf("got %+v, %v", stats, err)
	}
}

func TestExit(t *testing.T) {
	s := supervisor(t, true)
	if _, err := s.exit(Request{}); err == nil {
		t.Fatal("with nothing running there is no exit")
	}
	if err := s.do(request("first", 1)); err != nil {
		t.Fatal(err)
	}
	exit, err := s.exit(Request{})
	if err != nil || exit.IP != "203.0.113.7" || exit.Country != "NL" {
		t.Fatalf("got %+v, %v", exit, err)
	}
	// Another server is asked about through the running core.
	if exit, err := s.exit(Request{Server: request("other", 2).Server}); err != nil || exit.Country != "DE" {
		t.Fatalf("got %+v, %v", exit, err)
	}
	// A server the helper would not run is not given to the core either.
	bad := request("other", 2).Server
	bad.Proxy = map[string]any{"name": bad.Name, "type": "nonsense", "server": "x", "port": 1}
	if _, err := s.exit(Request{Server: bad}); err == nil {
		t.Fatal("a server outside the allow-list went to the core")
	}
}
