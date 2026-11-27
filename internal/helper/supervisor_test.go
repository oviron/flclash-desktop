package helper

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	out := json.NewEncoder(os.NewFile(3, "verdicts"))
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
