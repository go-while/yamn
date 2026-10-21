package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crooks/yamn/config"
)

// setupTestConfig points the cfg and flag globals at defaults from an empty
// config file.  They are restored when the test ends.
func setupTestConfig(t *testing.T) {
	t.Helper()
	oldCfg, oldFlag := cfg, flag
	t.Cleanup(func() { cfg, flag = oldCfg, oldFlag })
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "yamn.yml")
	if err := os.WriteFile(cfgFile, []byte("---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	flag = &config.Flags{Dir: dir, Config: cfgFile, NoDummy: true}
	cfg, err = flag.ParseConfig()
	if err != nil {
		t.Fatal(err)
	}
}

// newTestCert returns a self-signed certificate for host and a pool that
// trusts it.
func newTestCert(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		DNSNames:              []string{host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// fakeSMTP is a minimal SMTP server that records what a client did.
type fakeSMTP struct {
	ln        net.Listener
	tlsConf   *tls.Config // nil: STARTTLS is not offered
	offerAuth bool
	stall     string            // "greeting" or "MAIL": stop responding at that point
	closed    chan struct{}     // closed when a stalled client hangs up
	rcptReply map[string]string // RCPT replies by address (default "250 OK")
	dataReply string            // reply after the message (default "250 Queued")

	mu        sync.Mutex
	usedTLS   bool
	usedAuth  bool
	delivered bool
	rcpts     []string // RCPT TO addresses
	data      []byte   // message received with DATA
}

func newFakeSMTP(t *testing.T, cert *tls.Certificate, offerAuth bool, stall string) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{ln: ln, offerAuth: offerAuth, stall: stall, closed: make(chan struct{})}
	if cert != nil {
		s.tlsConf = &tls.Config{Certificates: []tls.Certificate{*cert}}
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	// Send every SMTP dial to this server, whatever host name is used.
	oldDial := smtpDial
	smtpDial = func(network, _ string) (net.Conn, error) {
		return net.Dial(network, ln.Addr().String())
	}
	t.Cleanup(func() { smtpDial = oldDial })
	return s
}

func (s *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	tp := textproto.NewConn(conn)
	inTLS := false
	if s.stall == "greeting" {
		s.waitForHangup(conn)
		return
	}
	tp.PrintfLine("220 fake ESMTP")
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			lines := []string{"fake"}
			if s.tlsConf != nil && !inTLS {
				lines = append(lines, "STARTTLS")
			}
			if s.offerAuth {
				lines = append(lines, "AUTH PLAIN")
			}
			for i, l := range lines {
				sep := "-"
				if i == len(lines)-1 {
					sep = " "
				}
				tp.PrintfLine("250%s%s", sep, l)
			}
		case cmd == "STARTTLS":
			tp.PrintfLine("220 Ready to start TLS")
			tlsConn := tls.Server(conn, s.tlsConf)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			tp = textproto.NewConn(conn)
			inTLS = true
			s.mu.Lock()
			s.usedTLS = true
			s.mu.Unlock()
		case strings.HasPrefix(cmd, "AUTH"):
			s.mu.Lock()
			s.usedAuth = true
			s.mu.Unlock()
			tp.PrintfLine("235 Authenticated")
		case strings.HasPrefix(cmd, "MAIL") && s.stall == "MAIL":
			s.waitForHangup(conn)
			return
		case strings.HasPrefix(cmd, "RCPT"):
			addr := strings.Trim(strings.TrimSpace(line[len("RCPT TO:"):]), "<>")
			s.mu.Lock()
			s.rcpts = append(s.rcpts, addr)
			reply := s.rcptReply[addr]
			s.mu.Unlock()
			if reply == "" {
				reply = "250 OK"
			}
			tp.PrintfLine("%s", reply)
		case strings.HasPrefix(cmd, "MAIL"):
			tp.PrintfLine("250 OK")
		case cmd == "DATA":
			tp.PrintfLine("354 Go ahead")
			data, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			s.mu.Lock()
			reply := s.dataReply
			if reply == "" {
				reply = "250 Queued"
				s.data = data
				s.delivered = true
			}
			s.mu.Unlock()
			tp.PrintfLine("%s", reply)
		case cmd == "QUIT":
			tp.PrintfLine("221 Bye")
			return
		default:
			tp.PrintfLine("502 Not implemented")
		}
	}
}

// waitForHangup stops responding and waits until the client closes the
// connection, like a tarpitting server.
func (s *fakeSMTP) waitForHangup(conn net.Conn) {
	io.Copy(io.Discard, conn)
	close(s.closed)
}

func (s *fakeSMTP) result() (usedTLS, usedAuth, delivered bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usedTLS, s.usedAuth, s.delivered
}

// TestSmtpSendTLS checks the TLS policy of smtpSend: the configured relay
// needs verified TLS, a recipient's MX gets opportunistic TLS unless
// require_tls is set, a relay on localhost may use plaintext or any
// certificate, and credentials only go to the configured relay.
func TestSmtpSendTLS(t *testing.T) {
	relayCert, _ := newTestCert(t, "relay.test")
	mxCert, _ := newTestCert(t, "mx.test")
	badRelayCert, _ := newTestCert(t, "relay.test")
	badMXCert, _ := newTestCert(t, "mx.test")
	trusted := x509.NewCertPool()
	trusted.AddCert(relayCert.Leaf)
	trusted.AddCert(mxCert.Leaf)

	tests := []struct {
		name       string
		host       string
		directMX   bool
		cert       *tls.Certificate // nil: server does not offer STARTTLS
		requireTLS bool
		wantErr    bool
		wantTLS    bool
		wantAuth   bool
		noCreds    bool // no SMTP username/password configured
	}{
		{"relay, trusted cert", "relay.test", false, &relayCert, false, false, true, true, false},
		{"relay, untrusted cert", "relay.test", false, &badRelayCert, false, true, false, false, false},
		{"relay, STARTTLS missing or stripped", "relay.test", false, nil, false, true, false, false, false},
		{"relay without login, STARTTLS missing or stripped", "relay.test", false, nil, false, true, false, false, true},
		{"MX, no STARTTLS", "mx.test", true, nil, false, false, false, false, false},
		{"MX, untrusted cert", "mx.test", true, &badMXCert, false, false, true, false, false},
		{"MX, require_tls, trusted cert", "mx.test", true, &mxCert, true, false, true, false, false},
		{"MX, require_tls, untrusted cert", "mx.test", true, &badMXCert, true, true, false, false, false},
		{"MX, require_tls, no STARTTLS", "mx.test", true, nil, true, true, false, false, false},
		{"localhost relay, no STARTTLS", "localhost", false, nil, false, false, false, true, false},
		{"localhost relay, self-signed cert", "localhost", false, &badRelayCert, false, false, true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestConfig(t)
			cfg.Mail.UseTLS = true
			cfg.Mail.RequireTLS = tt.requireTLS
			if !tt.noCreds {
				cfg.Mail.Username = "user"
				cfg.Mail.Password = "secret"
			}
			if !tt.directMX {
				cfg.Mail.SMTPRelay = tt.host
			}
			oldRoots := smtpRootCAs
			smtpRootCAs = trusted
			t.Cleanup(func() { smtpRootCAs = oldRoots })
			server := newFakeSMTP(t, tt.cert, true, "")

			err := smtpSend(tt.host, "25", tt.directMX, []byte("Subject: test\n\nbody\n"), []string{"rcpt@example.invalid"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("smtpSend error = %v, wantErr %v", err, tt.wantErr)
			}
			usedTLS, usedAuth, delivered := server.result()
			if delivered == tt.wantErr {
				t.Errorf("delivered = %v, want %v", delivered, !tt.wantErr)
			}
			if usedTLS != tt.wantTLS {
				t.Errorf("used TLS = %v, want %v", usedTLS, tt.wantTLS)
			}
			if usedAuth != tt.wantAuth {
				t.Errorf("sent credentials = %v, want %v", usedAuth, tt.wantAuth)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost":        true,
		"127.0.0.1":        true,
		"::1":              true,
		"relay.example":    false,
		"192.0.2.1":        false,
		"localhost.evil.x": false,
	} {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// TestSmtpSendTimeout checks that a server which stops responding, before
// its greeting or in the middle of the session, makes smtpSend give up after
// smtpTimeout and close the connection.
func TestSmtpSendTimeout(t *testing.T) {
	for _, stall := range []string{"greeting", "MAIL"} {
		t.Run("stall at "+stall, func(t *testing.T) {
			setupTestConfig(t)
			cfg.Mail.SMTPRelay = "localhost"
			oldTimeout := smtpTimeout
			smtpTimeout = 300 * time.Millisecond
			t.Cleanup(func() { smtpTimeout = oldTimeout })
			server := newFakeSMTP(t, nil, false, stall)

			start := time.Now()
			err := smtpSend("localhost", "25", false, []byte("Subject: test\n\nbody\n"), []string{"rcpt@example.invalid"})
			elapsed := time.Since(start)
			if err == nil {
				t.Fatal("smtpSend should fail when the server stops responding")
			}
			if elapsed > 5*time.Second {
				t.Fatalf("smtpSend took %s, expected about %s", elapsed, smtpTimeout)
			}
			select {
			case <-server.closed:
			case <-time.After(5 * time.Second):
				t.Fatal("smtpSend did not close the connection after the timeout")
			}
		})
	}
}

// TestSendmailUsesSmtpSend checks that sendmail mode goes through smtpSend,
// so it gets the relay TLS policy (and require_tls) and the timeouts.
func TestSendmailUsesSmtpSend(t *testing.T) {
	relayCert, pool := newTestCert(t, "relay.test")
	oldRoots := smtpRootCAs
	smtpRootCAs = pool
	t.Cleanup(func() { smtpRootCAs = oldRoots })

	t.Run("verified relay delivers", func(t *testing.T) {
		setupTestConfig(t)
		cfg.Mail.SMTPRelay = "relay.test"
		server := newFakeSMTP(t, &relayCert, false, "")
		if err := sendmail([]byte("Subject: test\n\nbody\n"), []string{"rcpt@example.invalid"}); err != nil {
			t.Fatalf("sendmail failed: %s", err)
		}
		if usedTLS, _, delivered := server.result(); !usedTLS || !delivered {
			t.Fatalf("expected delivery over TLS, got TLS=%v delivered=%v", usedTLS, delivered)
		}
	})
	t.Run("relay without STARTTLS is refused", func(t *testing.T) {
		setupTestConfig(t)
		cfg.Mail.SMTPRelay = "relay.test"
		server := newFakeSMTP(t, nil, false, "")
		if err := sendmail([]byte("Subject: test\n\nbody\n"), []string{"rcpt@example.invalid"}); err == nil {
			t.Fatal("sendmail should refuse a relay without STARTTLS")
		}
		if _, _, delivered := server.result(); delivered {
			t.Fatal("message should not be delivered in plaintext")
		}
	})
}
