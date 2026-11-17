package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureLog sends the log to a buffer for the rest of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &b
}

func TestAllowList(t *testing.T) {
	nodes := map[string]Node{
		"a": {Name: "a", Addr: "10.0.0.1:2525"},
		"b": {Name: "b", Addr: "[2001:db8::2]:25250"},
		"c": {Name: "c", Addr: "web-c.example:25250"}, // by name: not allowed by it
		"d": {Name: "d", Addr: "[::ffff:10.0.0.4]:2525"},
	}
	a, err := newAllowList(nodes, []string{"172.16.0.0/12", "198.51.100.7", "::ffff:203.0.113.0/120", "2001:db8:1::/48"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		remote string
		want   bool
	}{
		{"10.0.0.1:40000", true},
		{"[::ffff:10.0.0.1]:40000", true}, // IPv4-mapped form of a node
		{"10.0.0.4:1", true},              // node listed in mapped form
		{"[2001:db8::2]:1", true},
		{"172.17.0.2:1", true}, // -allow prefix
		{"198.51.100.7:1", true},
		{"203.0.113.9:1", true}, // -allow IPv4-mapped prefix, IPv4 client
		{"[::ffff:203.0.113.9]:1", true},
		{"[2001:db8:1:5::9]:1", true},
		{"127.0.0.1:1", true}, // loopback always
		{"127.0.0.9:1", true},
		{"[::1]:1", true},
		{"10.0.0.2:40000", false},
		{"[2001:db8::4]:1", false},
		{"198.51.100.8:1", false},
		{"203.0.114.1:1", false},
		{"[2001:db8:2::1]:1", false},
		{"[2001:db8::2%eth0]:1", false}, // zoned: never
		{"[fe80::1%eth0]:1", false},
		{"pipe", false}, // not host:port
		{"web-c.example:1", false},
		{"", false},
	} {
		if got := a.Allows(tc.remote); got != tc.want {
			t.Errorf("Allows(%q) = %v, want %v", tc.remote, got, tc.want)
		}
	}
	var none *allowList
	if !none.Allows("127.0.0.1:1") || none.Allows("10.0.0.1:1") {
		t.Error("a nil list should allow loopback only")
	}
	if s := a.String(); !strings.Contains(s, "10.0.0.1") || !strings.Contains(s, "172.16.0.0/12") || !strings.Contains(s, "203.0.113.0/24") {
		t.Errorf("String() = %q", s)
	}
	if len(a.byName) != 1 || !strings.Contains(a.byName[0], "web-c.example") {
		t.Errorf("byName = %q, want node c", a.byName)
	}
}

func TestAllowListErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		addr  string
		extra []string
	}{
		"node without port":        {"10.0.0.1", nil},
		"bad -allow":               {"10.0.0.1:2525", []string{"10.0.0.0/33"}},
		"name in -allow":           {"10.0.0.1:2525", []string{"web-c.example"}},
		"short mapped prefix":      {"10.0.0.1:2525", []string{"::ffff:0:0/95"}},
		"link-local node":          {"[fe80::1]:2525", nil},
		"zoned node":               {"[fe80::1%eth0]:2525", nil},
		"zoned global node":        {"[2001:db8::1%eth0]:2525", nil},
		"link-local -allow":        {"10.0.0.1:2525", []string{"fe80::1"}},
		"zoned -allow address":     {"10.0.0.1:2525", []string{"fe80::1%eth0"}},
		"link-local -allow prefix": {"10.0.0.1:2525", []string{"fe80::/64"}},
		"IPv4 link-local prefix":   {"10.0.0.1:2525", []string{"169.254.0.0/16"}},
		"IPv4 link-local node":     {"169.254.1.1:2525", nil},
		"malformed IPv4 node":      {"010.0.0.1:2525", nil},
		"short IPv4 node":          {"10.0.1:2525", nil},
	} {
		if _, err := newAllowList(map[string]Node{"a": {Name: "a", Addr: tc.addr}}, tc.extra); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestLoadAllow: the nodes file is read, and nodes listed by host name are
// named in the log, since they are only allowed through -allow.
func TestLoadAllow(t *testing.T) {
	logs := captureLog(t)
	file := filepath.Join(t.TempDir(), "nodes.txt")
	if err := os.WriteFile(file, []byte("a 10.0.0.1 hub\nb web-b.example exit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	nodes, al, err := loadAllow(file, "2525", []string{"192.0.2.2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || !al.Allows("10.0.0.1:1") || !al.Allows("192.0.2.2:1") {
		t.Errorf("nodes %v, list %s", nodes, al)
	}
	if !strings.Contains(logs.String(), "b (web-b.example)") {
		t.Errorf("node listed by name not logged:\n%s", logs)
	}
}

// TestRefusedLog: a stranger that keeps connecting gets one log line per
// interval, with the count; many sources together can't get one line per
// connection; and a newline in what it sends can't start a line of its own.
func TestRefusedLog(t *testing.T) {
	logs := captureLog(t)
	a, err := newAllowList(map[string]Node{"a": {Name: "a", Addr: "10.0.0.1:2525"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		a.Refused("203.0.113.9:4000", "smtp")
	}
	a.Refused("203.0.113.10:4000", "www")
	if n := strings.Count(logs.String(), "\n"); n != 2 {
		t.Errorf("%d log lines, want 2:\n%s", n, logs)
	}
	a.logMu.Lock()
	a.refused["203.0.113.9"].last = time.Now().Add(-refusedEvery - time.Second)
	a.logMu.Unlock()
	a.Refused("203.0.113.9:4000", "smtp")
	if !strings.Contains(logs.String(), "(50 times since the last such line)") {
		t.Errorf("count missing:\n%s", logs)
	}
	// More sources than the map holds: one overflow line, not one per source.
	logs.Reset()
	for i := 0; i < 3*maxRefusedLog; i++ {
		ip := netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, 14: byte(i >> 8), 15: byte(i)})
		a.Refused(netip.AddrPortFrom(ip, 1).String(), "smtp")
	}
	if n := strings.Count(logs.String(), "\n"); n > maxRefusedLog+1 {
		t.Errorf("%d log lines for %d sources, want at most %d", n, 3*maxRefusedLog, maxRefusedLog+1)
	}
	if !strings.Contains(logs.String(), "connections from more than") {
		t.Error("no overflow line")
	}
	// Once their interval is over, tracked sources make room again.
	a.logMu.Lock()
	for _, r := range a.refused {
		r.last = time.Now().Add(-refusedEvery - time.Second)
	}
	a.logMu.Unlock()
	logs.Reset()
	a.Refused("203.0.113.77:1", "smtp")
	if !strings.Contains(logs.String(), `"203.0.113.77"`) {
		t.Errorf("new source not logged after the old ones expired:\n%s", logs)
	}
	logs.Reset()
	a.Refused("x\n2026/01/01 00:00:00 forged", "smtp")
	if n := strings.Count(logs.String(), "\n"); n != 1 {
		t.Errorf("forged line in the log:\n%s", logs)
	}
	logs.Reset()
	h := logRequests(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r, _ := http.NewRequest("GET", "http://x/a%0a2026/01/01%2000:00:00%20forged", nil)
	r.RemoteAddr = "10.0.0.1:1"
	h.ServeHTTP(nil, r)
	if n := strings.Count(logs.String(), "\n"); n != 1 {
		t.Errorf("request path split the log line:\n%s", logs)
	}
}

// remoteConn gives one end of a net.Pipe a chosen remote address.
type remoteConn struct {
	net.Conn
	remote string
}

type fixedAddr string

func (a fixedAddr) Network() string { return "tcp" }
func (a fixedAddr) String() string  { return string(a) }

func (c remoteConn) RemoteAddr() net.Addr { return fixedAddr(c.remote) }

// pipeListener hands out connections given to dial, with the remote
// address the test chooses.
type pipeListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return fixedAddr("127.0.0.1:0") }

// dial connects to the listener as a client at remote.
func (l *pipeListener) dial(t *testing.T, remote string) net.Conn {
	t.Helper()
	server, client := net.Pipe()
	select {
	case l.conns <- remoteConn{server, remote}:
	case <-time.After(5 * time.Second):
		t.Fatal("listener didn't accept")
	}
	client.SetDeadline(time.Now().Add(5 * time.Second))
	t.Cleanup(func() { client.Close() })
	return client
}

func TestMXRefusesStrangers(t *testing.T) {
	captureLog(t)
	nodes := map[string]Node{
		"a": {Name: "a", Addr: "10.0.0.1:2525"},
		"b": {Name: "b", Addr: "10.0.0.2:2525"},
	}
	al, err := newAllowList(nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := newMX(nodes, "a", nil, filepath.Join(t.TempDir(), "spool"), al)
	if err != nil {
		t.Fatal(err)
	}
	ln := newPipeListener()
	go m.Serve(ln)
	t.Cleanup(func() { ln.Close() })
	greeting := func(remote string) string {
		line, err := bufio.NewReader(ln.dial(t, remote)).ReadString('\n')
		if err != nil {
			t.Fatalf("%s: %v", remote, err)
		}
		return strings.TrimSpace(line)
	}
	for remote, want := range map[string]string{
		"10.0.0.2:5000":    "220 ",
		"127.0.0.1:5000":   "220 ",
		"203.0.113.9:5000": "554 5.7.1 ",
		"pipe":             "554 5.7.1 ",
	} {
		if got := greeting(remote); !strings.HasPrefix(got, want) {
			t.Errorf("%s: got %q, want %q...", remote, got, want)
		}
	}
	// An mx without a list (as in the other tests) takes loopback only.
	m.Allow = nil
	if got := greeting("10.0.0.2:5000"); !strings.HasPrefix(got, "554 ") {
		t.Errorf("nil list, node address: got %q, want 554", got)
	}
}

func TestWWWRefusesStrangers(t *testing.T) {
	captureLog(t)
	al, err := newAllowList(map[string]Node{"a": {Name: "a", Addr: "10.0.0.1:2525"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ln := newPipeListener()
	go serveWWW(ln, al, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	t.Cleanup(func() { ln.Close() })
	get := func(remote string) (string, error) {
		c := ln.dial(t, remote)
		go io.WriteString(c, "GET /pubring.mix HTTP/1.1\r\nHost: x\r\n\r\n")
		resp, err := http.ReadResponse(bufio.NewReader(c), nil)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.Status + " " + string(body), nil
	}
	for _, remote := range []string{"10.0.0.1:1234", "127.0.0.1:1234"} {
		if got, err := get(remote); err != nil || got != "200 OK ok" {
			t.Errorf("%s: got %q, %v; want 200 OK ok", remote, got, err)
		}
	}
	// A stranger is disconnected without a byte: not even a 403.
	c := ln.dial(t, "203.0.113.9:1234")
	if n, err := c.Read(make([]byte, 1)); n != 0 || err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("stranger: read %d bytes, err %v; want a closed connection", n, err)
	}
}
