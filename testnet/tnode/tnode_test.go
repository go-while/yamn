package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crooks/yamn/keymgr"
	"github.com/dchest/blake2s"
)

func testNodes(t *testing.T) map[string]Node {
	t.Helper()
	f := filepath.Join(t.TempDir(), "nodes.txt")
	os.WriteFile(f, []byte("# test\na 127.0.0.1:1 exit,hub\nb 127.0.0.2 middle\n"), 0600)
	nodes, err := readNodes(f, "2525")
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}

func TestReadNodes(t *testing.T) {
	nodes := testNodes(t)
	if nodes["a"].Addr != "127.0.0.1:1" || nodes["b"].Addr != "127.0.0.2:2525" || nodes["b"].Domain() != "b.yamn.test" {
		t.Errorf("nodes: %+v", nodes)
	}
	f := filepath.Join(t.TempDir(), "dup.txt")
	os.WriteFile(f, []byte("a 1.2.3.4 exit\na 1.2.3.5 exit\n"), 0600)
	if _, err := readNodes(f, "2525"); err == nil {
		t.Error("a node listed twice was accepted")
	}
}

func TestRoute(t *testing.T) {
	nodes := testNodes(t)
	m := &MX{Self: nodes["a"], Nodes: nodes, Deliver: map[string]string{"pinger": "/p", "inbox": "/i"}}
	for _, c := range []struct {
		addr, local, node string
		ok                bool
	}{
		{"pinger+tok.123@a.yamn.test", "/p", "", true},
		{"Inbox@A.YAMN.TEST", "/i", "", true},
		{"mix@b.yamn.test", "", "b", true},
		{"nobody@a.yamn.test", "", "", false},
		{"x@c.yamn.test", "", "", false},
		{"x@example.org", "", "", false},
		{"x@yamn.test", "", "", false},
		{"noat", "", "", false},
	} {
		r, err := m.route(c.addr)
		if (err == nil) != c.ok || r.local != c.local || r.node != c.node {
			t.Errorf("%s: %+v, %v", c.addr, r, err)
		}
	}
}

// startMX runs an mx on a loopback port and returns it with its address.
func startMX(t *testing.T, name string, ln net.Listener, nodes map[string]Node, deliver map[string]string) *MX {
	t.Helper()
	m := &MX{Self: nodes[name], Nodes: nodes, Deliver: deliver, Spool: filepath.Join(t.TempDir(), "spool")}
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	go m.Serve(ln)
	t.Cleanup(func() { ln.Close() })
	return m
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func maildirFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "new", "*"))
	return files
}

// TestRelay sends mail to node a for a local inbox, for a pinger on node
// b and for an outside address: a delivers the first, forwards the second
// with its +token intact, and refuses the third.
func TestRelay(t *testing.T) {
	lnA, lnB := listen(t), listen(t)
	nodes := map[string]Node{
		"a": {Name: "a", Addr: lnA.Addr().String()},
		"b": {Name: "b", Addr: lnB.Addr().String()},
	}
	inbox, pinger := filepath.Join(t.TempDir(), "inbox"), filepath.Join(t.TempDir(), "pinger")
	a := startMX(t, "a", lnA, nodes, map[string]string{"inbox": inbox})
	startMX(t, "b", lnB, nodes, map[string]string{"pinger": pinger})

	c, err := smtp.Dial(lnA.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Hello("client.test"); err != nil {
		t.Fatal(err)
	}
	if err := c.Mail("pinger+bounce.x@b.yamn.test"); err != nil {
		t.Fatal(err)
	}
	if err := c.Rcpt("someone@example.org"); err == nil || !permanent(err) {
		t.Errorf("outside address: %v", err)
	}
	for _, r := range []string{"inbox@a.yamn.test", "pinger+tok.42@b.yamn.test"} {
		if err := c.Rcpt(r); err != nil {
			t.Fatalf("%s: %v", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("Subject: test\n\nhello\n.dot line\n"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	c.Quit()

	files := maildirFiles(t, inbox)
	if len(files) != 1 {
		t.Fatalf("inbox has %d mails", len(files))
	}
	got, _ := os.ReadFile(files[0])
	if !strings.Contains(string(got), "X-Original-To: inbox@a.yamn.test\n") || !strings.HasSuffix(string(got), "\nhello\n.dot line\n") {
		t.Errorf("inbox mail:\n%s", got)
	}
	if st, _ := os.Stat(files[0]); st.Mode().Perm() != 0660 {
		t.Errorf("mode %v, want 0660", st.Mode().Perm())
	}

	a.forwardDue(time.Now())
	deadline := time.Now().Add(5 * time.Second)
	for len(maildirFiles(t, pinger)) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	files = maildirFiles(t, pinger)
	if len(files) != 1 {
		t.Fatalf("pinger has %d mails", len(files))
	}
	got, _ = os.ReadFile(files[0])
	s := string(got)
	if !strings.Contains(s, "X-Original-To: pinger+tok.42@b.yamn.test\n") ||
		!strings.Contains(s, "Return-Path: <pinger+bounce.x@b.yamn.test>\n") ||
		strings.Count(s, "Received: ") != 2 || !strings.HasSuffix(s, "\nhello\n.dot line\n") {
		t.Errorf("pinger mail:\n%s", s)
	}
	if left, _ := filepath.Glob(filepath.Join(a.Spool, "*")); len(left) != 0 {
		t.Errorf("spool not empty: %v", left)
	}
}

// TestForwardRetry checks that mail for a node that's down stays spooled
// with a later retry time.
func TestForwardRetry(t *testing.T) {
	lnA, lnB := listen(t), listen(t)
	nodes := map[string]Node{
		"a": {Name: "a", Addr: lnA.Addr().String()},
		"b": {Name: "b", Addr: lnB.Addr().String()},
	}
	lnB.Close() // b is down
	a := startMX(t, "a", lnA, nodes, map[string]string{})
	now := time.Now()
	if _, err := a.accept("x@a.yamn.test", []rcpt{{addr: "mix@b.yamn.test", node: "b"}}, "h", "127.0.0.1", []byte("Subject: x\n\nbody\n")); err != nil {
		t.Fatal(err)
	}
	a.forwardDue(now)
	files, _ := filepath.Glob(filepath.Join(a.Spool, "*.json"))
	if len(files) != 1 {
		t.Fatalf("spool: %v", files)
	}
	b, _ := os.ReadFile(files[0])
	if !strings.Contains(string(b), `"Attempts": 1`) {
		t.Errorf("entry after a failed attempt:\n%s", b)
	}
}

func TestReceivedLoop(t *testing.T) {
	msg := strings.Repeat("Received: x\n", maxReceived+1) + "\nReceived: in the body\n"
	if n := received([]byte(msg)); n != maxReceived+1 {
		t.Errorf("received = %d", n)
	}
}

func TestPathArg(t *testing.T) {
	for _, c := range []struct {
		arg, prefix, want string
		ok                bool
	}{
		{"FROM:<a@b> BODY=8BITMIME", "FROM:", "a@b", true},
		{"from: <a@b>", "FROM:", "a@b", true},
		{"FROM:<>", "FROM:", "", true},
		{"TO:<a@b>", "FROM:", "", false},
		{"FROM:a@b", "FROM:", "", false},
		{"FR", "FROM:", "", false},
	} {
		got, ok := pathArg(c.arg, c.prefix)
		if got != c.want || ok != c.ok {
			t.Errorf("pathArg(%q) = %q, %v", c.arg, got, ok)
		}
	}
}

// TestMlist2 checks that the generated stats pass yamn's own check.
func TestMlist2(t *testing.T) {
	ring := "11101 mix@11101.yamn.test 0123456789abcdef0123456789abcdef 4:0.2.7+gw1 E 2026-10-25 2026-11-08\n\n" +
		"-----Begin Mix Key-----\nxx\n-----End Mix Key-----\n\n" +
		"11102 mix@11102.yamn.test 0123456789abcdef0123456789abcdee 4:0.2.7+gw1 M 2026-10-25 2026-11-08\n"
	names := pubringNames([]byte(ring))
	if strings.Join(names, ",") != "11101,11102" {
		t.Fatalf("names %v", names)
	}
	stats := mlist2For(names, time.Now())
	if err := keymgr.CheckStats(stats); err != nil {
		t.Errorf("yamn rejects the mlist2: %v\n%s", err, stats)
	}
}

// testKey returns a pubring entry with a keyid that matches its key, as
// yamn checks.
func testKey(name, caps string, seed byte) string {
	pk := make([]byte, 32)
	for i := range pk {
		pk[i] = seed
	}
	d, _ := blake2s.New(&blake2s.Config{Size: 16})
	d.Write(pk)
	id := hex.EncodeToString(d.Sum(nil))
	from := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	until := time.Now().UTC().AddDate(0, 0, 14).Format("2006-01-02")
	return fmt.Sprintf("%s mix@%s.yamn.test %s 4:0.2.7+gw1 %s %s %s\n\n-----Begin Mix Key-----\n%s\n%s\n-----End Mix Key-----\n\n",
		name, name, id, caps, from, until, id, hex.EncodeToString(pk))
}

// TestMlist2Candidates checks that with the generated stats, yamn's
// default chain criteria pick every remailer: exits for the last hop,
// all of them for the others.  A latency below stats.minlat once made
// the test network unable to build any chain.
func TestMlist2Candidates(t *testing.T) {
	dir := t.TempDir()
	ring := testKey("11101", "E", 1) + testKey("11102", "M", 2) + testKey("11104", "E", 4)
	os.WriteFile(filepath.Join(dir, "pubring.mix"), []byte(ring), 0644)
	if err := writeMlist2(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	p := keymgr.NewPubring(filepath.Join(dir, "pubring.mix"), filepath.Join(dir, "mlist2.txt"))
	if err := p.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	if err := p.ImportStats(); err != nil {
		t.Fatal(err)
	}
	// yamn's defaults: stats.minlat 2, maxlat 60, minrel 98, relfinal 99.
	if exits := p.Candidates(2, 60, 99, true); len(exits) != 2 {
		t.Errorf("exit candidates %v, want 11101 and 11104", exits)
	}
	if all := p.Candidates(2, 60, 98, false); len(all) != 3 {
		t.Errorf("candidates %v, want all 3", all)
	}
}
