package helper

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"strings"
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
	for {
		var size [4]byte
		if _, err := io.ReadFull(os.Stdin, size[:]); err != nil {
			return
		}
		config := make([]byte, binary.BigEndian.Uint32(size[:]))
		if _, err := io.ReadFull(os.Stdin, config); err != nil {
			return
		}
		reply := map[string]string{}
		if bytes.Contains(config, []byte("refuse-me")) {
			reply["error"] = "the core refused the config"
		}
		if bytes.Contains(config, []byte("die-now")) {
			os.Exit(3)
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
