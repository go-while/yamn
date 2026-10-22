package main

import (
	"crypto/sha3"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// testOnion returns a valid v3 onion address for a made-up key.
func testOnion(seed byte) string {
	raw := make([]byte, 35)
	for i := range 32 {
		raw[i] = seed + byte(i)
	}
	raw[34] = 3
	h := sha3.New256()
	h.Write([]byte(".onion checksum"))
	h.Write(raw[:32])
	h.Write([]byte{3})
	copy(raw[32:34], h.Sum(nil))
	return strings.ToLower(base32.StdEncoding.EncodeToString(raw)) + ".onion"
}

func TestValidOnion(t *testing.T) {
	good := testOnion(1)
	// One character changed: the checksum no longer matches.
	other := byte('a')
	if good[10] == 'a' {
		other = 'b'
	}
	typo := good[:10] + string(other) + good[11:]
	for host, want := range map[string]bool{
		// Tor Project and DuckDuckGo
		"2gzyxa5ihm7nsggfxnu52rck2vv4rvmdlkiu3zzui5du4xyclen53wid.onion": true,
		"duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion": true,
		good:                  true,
		strings.ToUpper(good): true,
		good + ".":            true,
		"mail." + good:        true,
		typo:                  false,
		"duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczae.onion": false,
		"expyuzz4wqqyqhjn.onion": false, // version 2
		"onion":                  false,
		".onion":                 false,
		"example.com":            false,
		good[:56] + ".onion.com": false,
		"0" + good[1:]:           false, // not base32
	} {
		if got := validOnion(host); got != want {
			t.Errorf("validOnion(%q) = %v, want %v", host, got, want)
		}
	}
	// The wrong version byte, with a matching checksum.
	raw, _ := base32.StdEncoding.DecodeString(strings.ToUpper(strings.TrimSuffix(good, ".onion")))
	raw[34] = 4
	h := sha3.New256()
	h.Write([]byte(".onion checksum"))
	h.Write(raw[:32])
	h.Write([]byte{4})
	copy(raw[32:34], h.Sum(nil))
	if v4 := strings.ToLower(base32.StdEncoding.EncodeToString(raw)) + ".onion"; validOnion(v4) {
		t.Errorf("validOnion(%q) with version 4 = true", v4)
	}
}

// fakeSOCKS is a SOCKS5 proxy that records what it's asked for and
// connects every request to target.
type fakeSOCKS struct {
	ln     net.Listener
	target string
	reply  byte // CONNECT reply code; 0 connects to target
	noAuth bool // choose "no authentication" instead of a login

	mu      sync.Mutex
	users   []string // user names of the logins
	methods [][]byte // offered methods per greeting
	hosts   []string // host:port of the CONNECT requests
	atyps   []byte   // their address types
}

func newFakeSOCKS(t *testing.T, target string) *fakeSOCKS {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	p := &fakeSOCKS{ln: ln, target: target}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go p.serve(conn)
		}
	}()
	return p
}

func (p *fakeSOCKS) serve(conn net.Conn) {
	defer conn.Close()
	read := func(n int) []byte {
		b := make([]byte, n)
		if _, err := io.ReadFull(conn, b); err != nil {
			return nil
		}
		return b
	}
	greet := read(2)
	if greet == nil || greet[0] != 5 {
		return
	}
	methods := read(int(greet[1]))
	p.mu.Lock()
	p.methods = append(p.methods, methods)
	p.mu.Unlock()
	if p.noAuth || !slices.Contains(methods, 2) {
		conn.Write([]byte{5, 0})
	} else {
		conn.Write([]byte{5, 2})
		head := read(2)
		if head == nil {
			return
		}
		user := read(int(head[1]))
		plen := read(1)
		if plen == nil || read(int(plen[0])) == nil {
			return
		}
		p.mu.Lock()
		p.users = append(p.users, string(user))
		p.mu.Unlock()
		conn.Write([]byte{1, 0})
	}
	req := read(4)
	if req == nil || req[1] != 1 {
		return
	}
	var host string
	switch req[3] {
	case 3:
		n := read(1)
		host = string(read(int(n[0])))
	case 1:
		host = net.IP(read(4)).String()
	case 4:
		host = net.IP(read(16)).String()
	}
	port := binary.BigEndian.Uint16(read(2))
	p.mu.Lock()
	p.hosts = append(p.hosts, net.JoinHostPort(host, strconv.Itoa(int(port))))
	p.atyps = append(p.atyps, req[3])
	p.mu.Unlock()
	if p.reply != 0 {
		conn.Write([]byte{5, p.reply, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	up, err := net.Dial("tcp", p.target)
	if err != nil {
		conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()
	// Reply with a domain name as the bound address, to test skipping it.
	conn.Write(append([]byte{5, 0, 0, 3, 4}, "tor!\x00\x00"...))
	go io.Copy(up, conn)
	io.Copy(conn, up)
}

// TestTorDial checks the SOCKS5 exchange: the host name goes to the proxy
// unresolved, and every connection logs in with its own user name.
func TestTorDial(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { echo.Close() })
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	proxy := newFakeSOCKS(t, echo.Addr().String())
	onion := testOnion(7)
	for range 2 {
		conn, err := torDial(proxy.ln.Addr().String(), net.JoinHostPort(onion, "25"), 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.Write([]byte("hello"))
		buf := make([]byte, 5)
		if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "hello" {
			t.Errorf("data through the proxy: %q, %v", buf, err)
		}
		conn.Close()
	}
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	if want := []string{onion + ":25", onion + ":25"}; !slices.Equal(proxy.hosts, want) {
		t.Errorf("CONNECT to %q, want %q", proxy.hosts, want)
	}
	if !slices.Equal(proxy.atyps, []byte{3, 3}) {
		t.Errorf("address types %v, want domain names (3)", proxy.atyps)
	}
	if len(proxy.users) != 2 || proxy.users[0] == proxy.users[1] || len(proxy.users[0]) < 16 {
		t.Errorf("user names %q, want two different random ones", proxy.users)
	}
}

// TestTorDialErrors checks that a refused CONNECT, a proxy that isn't
// there and one that doesn't answer are errors in time.
func TestTorDialErrors(t *testing.T) {
	proxy := newFakeSOCKS(t, "127.0.0.1:1")
	proxy.reply = 0xf0
	_, err := torDial(proxy.ln.Addr().String(), testOnion(1)+":25", 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "descriptor not found") {
		t.Errorf("refused CONNECT: got %v", err)
	}
	if errors.Is(err, errPermanent) {
		t.Errorf("an onion service that's down should be retried: %v", err)
	}
	proxy.noAuth = true
	proxy.reply = 0
	proxy.target = "127.0.0.1:1"
	if _, err = torDial(proxy.ln.Addr().String(), testOnion(1)+":25", 5*time.Second); err == nil {
		t.Error("a CONNECT the proxy can't complete should be an error")
	}

	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := closed.Addr().String()
	closed.Close()
	if _, err = torDial(addr, testOnion(1)+":25", 5*time.Second); err == nil || !strings.Contains(err.Error(), "tor_socks") {
		t.Errorf("no proxy: got %v", err)
	}

	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { silent.Close() })
	go func() {
		for {
			c, err := silent.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	start := time.Now()
	if _, err = torDial(silent.Addr().String(), testOnion(1)+":25", 300*time.Millisecond); err == nil {
		t.Error("a proxy that doesn't answer should be an error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("torDial took %s", d)
	}
}

// setupTorDelivery points mail.tor_socks at a fake proxy that connects to
// server, and makes every DNS lookup and every direct dial of a .onion host
// a test failure.
func setupTorDelivery(t *testing.T, server *fakeSMTP) *fakeSOCKS {
	t.Helper()
	proxy := newFakeSOCKS(t, server.ln.Addr().String())
	cfg.Mail.TorSocks = proxy.ln.Addr().String()
	oldMX, oldHost := lookupMX, lookupHost
	t.Cleanup(func() { lookupMX, lookupHost = oldMX, oldHost })
	lookupMX = func(domain string) ([]*net.MX, error) {
		if isOnion(domain) {
			t.Errorf("MX lookup of %s", domain)
		}
		return []*net.MX{{Host: "mx." + domain + ".", Pref: 10}}, nil
	}
	lookupHost = func(host string) ([]string, error) {
		t.Errorf("address lookup of %s", host)
		return nil, errors.New("no")
	}
	return proxy
}

// TestOnionRecipient checks that mail to a .onion domain goes to that
// onion service on port 25 through tor_socks, without DNS, and that mail to
// other domains still goes directly to their MX.
func TestOnionRecipient(t *testing.T) {
	server := setupMXDelivery(t, nil)
	proxy := setupTorDelivery(t, server)
	onion := testOnion(3)
	file := writePoolMessage(t, "To: alice@"+strings.ToUpper(onion)+"\nCc: bob@clear.invalid\n")
	emailPoolFile(filepath.Base(file))
	if _, err := os.Stat(file); err == nil {
		t.Error("pool file kept after every recipient got the message")
	}
	proxy.mu.Lock()
	hosts := proxy.hosts
	proxy.mu.Unlock()
	if want := []string{onion + ":25"}; !slices.Equal(hosts, want) {
		t.Errorf("through Tor: %q, want %q", hosts, want)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if want := []string{"mx.clear.invalid:25"}; !slices.Equal(server.dialed, want) {
		t.Errorf("dialed directly: %q, want %q", server.dialed, want)
	}
	if len(server.sessions) != 2 {
		t.Errorf("sessions %q, want one per domain", server.sessions)
	}
}

// TestOnionRecipientWithoutTor checks that without tor_socks, a .onion
// recipient is refused for good, as is an invalid onion address with it.
func TestOnionRecipientWithoutTor(t *testing.T) {
	for _, tt := range []struct{ torSocks, domain string }{
		{"", testOnion(3)},
		{"127.0.0.1:9", "expyuzz4wqqyqhjn.onion"},
	} {
		server := setupMXDelivery(t, nil)
		cfg.Mail.TorSocks = tt.torSocks
		file := writePoolMessage(t, "To: alice@"+tt.domain+"\n")
		del, err := mailPoolFile(file)
		if !del || !errors.Is(err, errPermanent) {
			t.Errorf("tor_socks %q, %s: delete %v, error %v; want a permanent error", tt.torSocks, tt.domain, del, err)
		}
		server.mu.Lock()
		if len(server.dialed) > 0 {
			t.Errorf("dialed %q", server.dialed)
		}
		server.mu.Unlock()
	}
}

// TestOnionMX checks that a .onion MX is used through tor_socks, and
// skipped without it.
func TestOnionMX(t *testing.T) {
	onion := testOnion(5)
	for _, withTor := range []bool{true, false} {
		server := setupMXDelivery(t, nil)
		var proxy *fakeSOCKS
		if withTor {
			proxy = setupTorDelivery(t, server)
		}
		lookupMX = func(domain string) ([]*net.MX, error) {
			return []*net.MX{{Host: onion + ".", Pref: 0}, {Host: "mx.example.invalid.", Pref: 10}}, nil
		}
		file := writePoolMessage(t, "To: alice@example.invalid\n")
		emailPoolFile(filepath.Base(file))
		server.mu.Lock()
		dialed, delivered := server.dialed, server.delivered
		server.mu.Unlock()
		if !delivered {
			t.Errorf("tor %v: not delivered", withTor)
		}
		if withTor {
			proxy.mu.Lock()
			if want := []string{onion + ":25"}; !slices.Equal(proxy.hosts, want) {
				t.Errorf("through Tor: %q, want %q", proxy.hosts, want)
			}
			proxy.mu.Unlock()
			if len(dialed) > 0 {
				t.Errorf("dialed directly: %q", dialed)
			}
		} else if want := []string{"mx.example.invalid:25"}; !slices.Equal(dialed, want) {
			t.Errorf("without Tor: dialed %q, want %q", dialed, want)
		}
	}
}

// TestOnionViaRelay checks that with smtp_relay, .onion recipients go
// through Tor and the others to the relay, in one session.
func TestOnionViaRelay(t *testing.T) {
	server := setupDeliveryTest(t)
	proxy := setupTorDelivery(t, server)
	onion := testOnion(9)
	file := writePoolMessage(t, "To: a@one.invalid, o@"+onion+"\nCc: b@two.invalid\n")
	emailPoolFile(filepath.Base(file))
	if _, err := os.Stat(file); err == nil {
		t.Error("pool file kept after every recipient got the message")
	}
	proxy.mu.Lock()
	if want := []string{onion + ":25"}; !slices.Equal(proxy.hosts, want) {
		t.Errorf("through Tor: %q, want %q", proxy.hosts, want)
	}
	proxy.mu.Unlock()
	server.mu.Lock()
	defer server.mu.Unlock()
	if want := []string{"localhost:587"}; !slices.Equal(server.dialed, want) {
		t.Errorf("dialed directly: %q, want %q", server.dialed, want)
	}
	want := [][]string{{"a@one.invalid", "b@two.invalid"}, {"o@" + onion}}
	if !slices.EqualFunc(server.sessions, want, slices.Equal) {
		t.Errorf("sessions %q, want %q", server.sessions, want)
	}
}

// TestOnionViaRelayPartial checks that when the onion service is down, the
// relay's recipients are noted as done and only the onion one is retried.
func TestOnionViaRelayPartial(t *testing.T) {
	server := setupDeliveryTest(t)
	proxy := setupTorDelivery(t, server)
	proxy.reply = 0xf0
	onion := testOnion(9)
	file := writePoolMessage(t, "To: a@one.invalid, o@"+onion+"\n")
	if _, err := mailPoolFile(file); err == nil || errors.Is(err, errPermanent) {
		t.Errorf("want a temporary error, got %v", err)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("pool file not kept: %v", err)
	}
	if _, done, _ := parseInternalHeader(content); !slices.Equal(done, []string{"a@one.invalid"}) {
		t.Errorf("done %q, want the relay's recipient", done)
	}
}

// TestOnionTLS checks that an onion service without STARTTLS gets the mail
// even with require_tls, that one with a self-signed certificate is
// accepted, and that a login to a .onion smtp_relay works without TLS.
func TestOnionTLS(t *testing.T) {
	cert, _ := newTestCert(t, "elsewhere.invalid")
	for _, tt := range []struct {
		name  string
		cert  bool
		relay bool
	}{
		{"no STARTTLS", false, false},
		{"self-signed", true, false},
		{"relay login", false, true},
	} {
		setupTestConfig(t)
		cfg.Mail.RequireTLS = true
		var server *fakeSMTP
		if tt.cert {
			server = newFakeSMTP(t, &cert, tt.relay, "")
		} else {
			server = newFakeSMTP(t, nil, tt.relay, "")
		}
		setupTorDelivery(t, server)
		onion := testOnion(11)
		var err error
		if tt.relay {
			cfg.Mail.SMTPRelay = onion
			cfg.Mail.Username, cfg.Mail.Password = "user", "secret"
			_, err = smtpSend(onion, "587", false, []byte("Subject: x\n\nx\n"), []string{"a@example.invalid"})
		} else {
			_, err = smtpSend(onion, "25", true, []byte("Subject: x\n\nx\n"), []string{"a@" + onion})
		}
		usedTLS, usedAuth, delivered := server.result()
		if err != nil || !delivered {
			t.Errorf("%s: delivered %v, error %v", tt.name, delivered, err)
		}
		if usedTLS != tt.cert {
			t.Errorf("%s: used TLS %v, want %v", tt.name, usedTLS, tt.cert)
		}
		if usedAuth != tt.relay {
			t.Errorf("%s: logged in %v, want %v", tt.name, usedAuth, tt.relay)
		}
	}
}

// TestOnionRelayWithoutTor checks that a .onion smtp_relay without
// tor_socks is a temporary error (a configuration problem), not a dial.
func TestOnionRelayWithoutTor(t *testing.T) {
	server := setupDeliveryTest(t)
	cfg.Mail.SMTPRelay = testOnion(1)
	file := writePoolMessage(t, "To: a@example.invalid\n")
	if del, err := mailPoolFile(file); del || err == nil || errors.Is(err, errPermanent) {
		t.Errorf("delete %v, error %v; want a temporary error", del, err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.dialed) > 0 {
		t.Errorf("dialed %q", server.dialed)
	}
}

func TestNagTor(t *testing.T) {
	for _, tt := range []struct {
		onionRelay bool
		torSocks   string
		pipe       string
		want       string
	}{
		{false, "", "", ""},
		{true, "", "", "onion_relay is ignored"},
		{true, "127.0.0.1:9050", "", ""},
		{false, "[::1]:9050", "", ""},
		{false, "localhost:9050", "", ""},
		{false, "10.0.0.5:9050", "", "isn't on this machine"},
		{false, "127.0.0.1:9050", "/usr/sbin/sendmail -i", "no effect"},
	} {
		setupTestConfig(t)
		logged := captureLog(t)
		cfg.Mail.OnionRelay, cfg.Mail.TorSocks, cfg.Mail.Pipe = tt.onionRelay, tt.torSocks, tt.pipe
		nagOperator()
		got := logged.String()
		if tt.want == "" && (strings.Contains(got, "onion") || strings.Contains(got, "tor_socks")) {
			t.Errorf("%+v: unexpected warning:\n%s", tt, got)
		}
		if tt.want != "" && !strings.Contains(got, tt.want) {
			t.Errorf("%+v: want %q in:\n%s", tt, tt.want, got)
		}
	}
}
