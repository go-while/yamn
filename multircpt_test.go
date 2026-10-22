package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// setupMXDelivery is setupDeliveryTest with mx_relay on.  Every domain's MX
// is "mx.<domain>", unless failDomains says the lookup fails for it.
func setupMXDelivery(t *testing.T, failDomains map[string]bool) *fakeSMTP {
	t.Helper()
	server := setupDeliveryTest(t)
	cfg.Mail.MXRelay = true
	oldMX := lookupMX
	t.Cleanup(func() { lookupMX = oldMX })
	lookupMX = func(domain string) ([]*net.MX, error) {
		if failDomains[domain] {
			return nil, &net.DNSError{Err: "server misbehaving", IsTemporary: true}
		}
		return []*net.MX{{Host: "mx." + domain + ".", Pref: 10}}, nil
	}
	return server
}

// TestMXSendPerDomain checks that a message with several recipients goes to
// each domain's MX, in one session per domain with only its own recipients,
// and not to smtp_relay.
func TestMXSendPerDomain(t *testing.T) {
	server := setupMXDelivery(t, nil)
	cfg.Mail.SMTPRelay = "relay.invalid"
	file := writePoolMessage(t, "To: a@one.invalid\nCc: c@two.invalid\nBcc: b@ONE.invalid\n")
	emailPoolFile(filepath.Base(file))
	if _, err := os.Stat(file); err == nil {
		t.Error("pool file kept after every recipient got the message")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	wantDialed := []string{"mx.one.invalid:25", "mx.two.invalid:25"}
	if !slices.Equal(server.dialed, wantDialed) {
		t.Errorf("dialed %q, want %q", server.dialed, wantDialed)
	}
	want := [][]string{{"a@one.invalid", "b@ONE.invalid"}, {"c@two.invalid"}}
	if !slices.EqualFunc(server.sessions, want, slices.Equal) {
		t.Errorf("sessions %q, want %q", server.sessions, want)
	}
	if bytes.Contains(server.data, []byte("Yamn-")) {
		t.Errorf("internal header delivered:\n%s", server.data)
	}
}

// TestMXSendPartialRetry checks that after one domain got the message and
// another failed for now, the pool file notes the done recipients, keeps its
// pooled date, and the next run only sends to the rest.
func TestMXSendPartialRetry(t *testing.T) {
	server := setupMXDelivery(t, map[string]bool{"two.invalid": true})
	file := writePoolMessage(t, "To: a@one.invalid, c@two.invalid\n")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// A pool file from three days ago, to see that the date is kept.
	date := time.Now().AddDate(0, 0, -3).Format(shortdate)
	before = append(internalHeaderLine(date, nil), stripInternalHeader(before)...)
	if err := os.WriteFile(file, before, 0600); err != nil {
		t.Fatal(err)
	}

	emailPoolFile(filepath.Base(file))
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("pool file should be kept for c@two.invalid: %s", err)
	}
	gotDate, done, _ := parseInternalHeader(after)
	if gotDate != date || !slices.Equal(done, []string{"a@one.invalid"}) {
		t.Errorf("first line %q: want date %q and done a@one.invalid", strings.SplitN(string(after), "\n", 2)[0], date)
	}
	if !bytes.Equal(stripInternalHeader(after), stripInternalHeader(before)) {
		t.Error("the message itself changed")
	}
	if age := poolFileAge(file, after); age != 3 {
		t.Errorf("pool file age %d, want 3", age)
	}

	// Next run: DNS works again.
	lookupMX = func(domain string) ([]*net.MX, error) {
		return []*net.MX{{Host: "mx." + domain + ".", Pref: 10}}, nil
	}
	server.mu.Lock()
	server.sessions = nil
	server.mu.Unlock()
	emailPoolFile(filepath.Base(file))
	if _, err := os.Stat(file); err == nil {
		t.Error("pool file kept after the last recipient got it")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if want := [][]string{{"c@two.invalid"}}; !slices.EqualFunc(server.sessions, want, slices.Equal) {
		t.Errorf("second run sessions %q, want %q", server.sessions, want)
	}
}

// TestPoolDoneNotForgeable checks that a done list in the sender's own
// headers is ignored: only yamn's first line counts.
func TestPoolDoneNotForgeable(t *testing.T) {
	server := setupDeliveryTest(t)
	file := writePoolMessage(t, "Yamn-Pooled-Date: 1 Jan 2020; done=a@example.invalid\nTo: a@example.invalid\n")
	emailPoolFile(filepath.Base(file))
	server.mu.Lock()
	defer server.mu.Unlock()
	if want := [][]string{{"a@example.invalid"}}; !slices.EqualFunc(server.sessions, want, slices.Equal) {
		t.Errorf("sessions %q, want %q", server.sessions, want)
	}
}

func TestParseInternalHeader(t *testing.T) {
	for _, tt := range []struct {
		content string
		date    string
		done    []string
		ok      bool
	}{
		{"Yamn-Pooled-Date: 22 Oct 2026\nTo: x\n", "22 Oct 2026", nil, true},
		{"Yamn-Pooled-Date: 22 Oct 2026; done=a@x.invalid,b@y.invalid\nTo: x\n", "22 Oct 2026", []string{"a@x.invalid", "b@y.invalid"}, true},
		{"To: x\n", "", nil, false},
	} {
		date, done, ok := parseInternalHeader([]byte(tt.content))
		if date != tt.date || !slices.Equal(done, tt.done) || ok != tt.ok {
			t.Errorf("%q: got %q %q %v", tt.content, date, done, ok)
		}
		if ok {
			if line := internalHeaderLine(date, done); !strings.HasPrefix(tt.content, string(line)) {
				t.Errorf("internalHeaderLine(%q, %q) = %q", date, done, line)
			}
		}
	}
}
