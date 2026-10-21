package main

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// setupDeliveryTest gives the test an empty pool and a fake SMTP server.
func setupDeliveryTest(t *testing.T) *fakeSMTP {
	t.Helper()
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Pooldir = t.TempDir()
	cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
	return newFakeSMTP(t, nil, false, "")
}

// TestDeliveryFailures checks which SMTP failures keep a pool file for the
// next run: only temporary ones (4xx, network).  A 5xx is permanent and the
// file is deleted, and a recipient rejected with 5xx doesn't stop delivery
// to the others.
func TestDeliveryFailures(t *testing.T) {
	tests := []struct {
		name          string
		to            string
		rcptReply     map[string]string
		dataReply     string
		noServer      bool
		wantDelivered bool
		wantKept      bool
		wantRcpts     []string // recipients of the delivered message
	}{
		{name: "accepted", to: "a@example.invalid", wantDelivered: true, wantRcpts: []string{"a@example.invalid"}},
		{name: "recipient rejected", to: "a@example.invalid",
			rcptReply: map[string]string{"a@example.invalid": "550 5.1.1 no such user"}},
		{name: "one of two rejected", to: "a@example.invalid, b@example.invalid",
			rcptReply:     map[string]string{"a@example.invalid": "550 5.1.1 no such user"},
			wantDelivered: true, wantRcpts: []string{"b@example.invalid"}},
		{name: "recipient deferred", to: "a@example.invalid, b@example.invalid",
			rcptReply: map[string]string{"b@example.invalid": "451 4.7.1 greylisted"},
			wantKept:  true},
		{name: "message rejected", to: "a@example.invalid", dataReply: "554 5.1.0 bad recipient"},
		// From the relay, a policy 5xx usually means a misconfiguration
		// (relaying denied, missing login): keep the mail.
		{name: "relaying denied by relay", to: "a@example.invalid",
			rcptReply: map[string]string{"a@example.invalid": "554 5.7.1 relaying denied"},
			wantKept:  true},
		{name: "message refused by relay policy", to: "a@example.invalid", dataReply: "554 5.7.1 spam", wantKept: true},
		{name: "message deferred", to: "a@example.invalid", dataReply: "452 4.3.1 out of space", wantKept: true},
		{name: "no server", to: "a@example.invalid", noServer: true, wantKept: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupDeliveryTest(t)
			server.mu.Lock()
			server.rcptReply = tt.rcptReply
			server.dataReply = tt.dataReply
			server.mu.Unlock()
			if tt.noServer {
				oldDial := smtpDial
				smtpDial = func(network, _ string) (net.Conn, error) {
					return net.Dial(network, "127.0.0.1:1")
				}
				t.Cleanup(func() { smtpDial = oldDial })
			}
			file := writePoolMessage(t, "To: "+tt.to+"\n")
			emailPoolFile(filepath.Base(file))
			_, err := os.Stat(file)
			if kept := err == nil; kept != tt.wantKept {
				t.Errorf("pool file kept = %v, want %v", kept, tt.wantKept)
			}
			_, _, delivered := server.result()
			if delivered != tt.wantDelivered {
				t.Fatalf("delivered = %v, want %v", delivered, tt.wantDelivered)
			}
			if !delivered {
				return
			}
			server.mu.Lock()
			var accepted []string
			for _, r := range server.rcpts {
				if server.rcptReply[r] == "" {
					accepted = append(accepted, r)
				}
			}
			server.mu.Unlock()
			if !slices.Equal(accepted, tt.wantRcpts) {
				t.Errorf("accepted recipients = %q, want %q", accepted, tt.wantRcpts)
			}
		})
	}
}

// TestMXLookupFailures checks direct MX delivery when DNS has no usable
// answer: a null MX (RFC 7505) or a domain that doesn't exist is permanent,
// a temporary DNS failure keeps the file, and only a domain without MX
// records falls back to its own address.
func TestMXLookupFailures(t *testing.T) {
	notFound := &net.DNSError{Err: "no such host", IsNotFound: true}
	servFail := &net.DNSError{Err: "server misbehaving", IsTemporary: true}
	tests := []struct {
		name          string
		to            string // default a@example.invalid
		mx            []*net.MX
		mxErr         error
		hostErr       error
		wantDelivered bool
		wantKept      bool
	}{
		{name: "MX", mx: []*net.MX{{Host: "mx.example.invalid.", Pref: 10}}, wantDelivered: true},
		{name: "null MX", mx: []*net.MX{{Host: ".", Pref: 0}}},
		{name: "only onion MX", mx: []*net.MX{{Host: "abc.onion.", Pref: 0}}},
		{name: "only onion MX, upper case", mx: []*net.MX{{Host: "ABC.Onion.", Pref: 0}}},
		{name: "onion MX first", mx: []*net.MX{{Host: "abc.onion.", Pref: 0}, {Host: "mx.example.invalid.", Pref: 10}}, wantDelivered: true},
		{name: "onion recipient", to: "a@abc.ONION", mxErr: servFail},
		{name: "temporary DNS failure", mxErr: servFail, wantKept: true},
		{name: "no MX, address found", mxErr: notFound, wantDelivered: true},
		{name: "no MX, no address (NXDOMAIN)", mxErr: notFound, hostErr: notFound},
		{name: "no MX, address lookup fails", mxErr: notFound, hostErr: servFail, wantKept: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupDeliveryTest(t)
			cfg.Mail.MXRelay = true
			oldMX, oldHost := lookupMX, lookupHost
			t.Cleanup(func() { lookupMX, lookupHost = oldMX, oldHost })
			lookupMX = func(string) ([]*net.MX, error) { return tt.mx, tt.mxErr }
			lookupHost = func(string) ([]string, error) {
				if tt.hostErr != nil {
					return nil, tt.hostErr
				}
				return []string{"192.0.2.1"}, nil
			}
			to := tt.to
			if to == "" {
				to = "a@example.invalid"
			}
			file := writePoolMessage(t, "To: "+to+"\n")
			emailPoolFile(filepath.Base(file))
			_, err := os.Stat(file)
			if kept := err == nil; kept != tt.wantKept {
				t.Errorf("pool file kept = %v, want %v", kept, tt.wantKept)
			}
			if _, _, delivered := server.result(); delivered != tt.wantDelivered {
				t.Errorf("delivered = %v, want %v", delivered, tt.wantDelivered)
			}
		})
	}
}

// TestDeliveryFailuresDirectMX checks that every 5xx from the recipient's
// own mail server is permanent, including policy rejections that the
// configured relay would get a retry for.
func TestDeliveryFailuresDirectMX(t *testing.T) {
	tests := []struct {
		name      string
		rcptReply map[string]string
		dataReply string
	}{
		{name: "recipient refused by policy", rcptReply: map[string]string{"a@example.invalid": "550 5.7.1 refused"}},
		{name: "message refused by policy", dataReply: "554 5.7.1 spam"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupDeliveryTest(t)
			cfg.Mail.MXRelay = true
			oldMX := lookupMX
			t.Cleanup(func() { lookupMX = oldMX })
			lookupMX = func(string) ([]*net.MX, error) {
				return []*net.MX{{Host: "mx.example.invalid.", Pref: 10}}, nil
			}
			server.mu.Lock()
			server.rcptReply = tt.rcptReply
			server.dataReply = tt.dataReply
			server.mu.Unlock()
			file := writePoolMessage(t, "To: a@example.invalid\n")
			emailPoolFile(filepath.Base(file))
			if _, err := os.Stat(file); err == nil {
				t.Error("pool file kept after a 5xx from the recipient's MX")
			}
		})
	}
}
