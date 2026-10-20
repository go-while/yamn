package main

import (
	"bytes"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFilterHeaders(t *testing.T) {
	setupTestConfig(t)
	cfg.Mail.AllowHeaders = []string{"x-custom"}
	h := mail.Header{
		"To":           {"a@example.invalid"},
		"Subject":      {"hi"},
		"Content-Type": {"text/plain"},
		"Newsgroups":   {"alt.test"},
		"X-Custom":     {"allowed by config"},
		"Bcc":          {"b@example.invalid"},
		"User-Agent":   {"Leaky/1.0"},
		"X-Mailer":     {"Leaky"},
		"Sender":       {"someone@example.invalid"},
		"Resent-From":  {"someone@example.invalid"},
		"Received":     {"from host"},
	}
	filterHeaders(h)
	var got []string
	for k := range h {
		got = append(got, k)
	}
	slices.Sort(got)
	want := []string{"Content-Type", "Newsgroups", "Subject", "To", "X-Custom"}
	if !slices.Equal(got, want) {
		t.Fatalf("Headers after filtering: got %q, want %q", got, want)
	}
}

func TestParseFromQuotesName(t *testing.T) {
	setupTestConfig(t)
	cfg.Mail.CustomFrom = false
	cfg.Mail.OutboundAddy = "remailer@exit.invalid"
	h := mail.Header{"From": {`"Bank <security@bank.example>" <someone@example.invalid>`}}
	from := parseFrom(h)[0]
	addr, err := mail.ParseAddress(from)
	if err != nil {
		t.Fatalf("parseFrom produced an invalid address %q: %s", from, err)
	}
	if addr.Address != "remailer@exit.invalid" {
		t.Errorf("From address: got %q, want the outbound address", addr.Address)
	}
	if addr.Name != "Bank <security@bank.example>" {
		t.Errorf("From display name: got %q", addr.Name)
	}
}

// TestMailPoolFileHeaders sends a pooled exit message through mailPoolFile to
// a fake SMTP server and checks recipients and headers of what's delivered.
func TestMailPoolFileHeaders(t *testing.T) {
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Mail.CustomFrom = false
	cfg.Mail.OutboundAddy = "remailer@exit.invalid"
	cfg.Files.Pooldir = t.TempDir()
	server := newFakeSMTP(t, nil, false, "")

	poolFile := filepath.Join(cfg.Files.Pooldir, "mtest")
	msg := "Yamn-Pooled-Date: " + time.Now().Format(shortdate) + "\n" +
		"To: alice@example.invalid\n" +
		"To: dave@example.invalid\n" +
		"Cc: carol@example.invalid\n" +
		"Bcc: bob@example.invalid, alice@example.invalid\n" +
		"Subject: hi\n" +
		"User-Agent: Leaky/1.0\n" +
		"Sender: someone@example.invalid\n" +
		"Content-Type: text/plain\n" +
		`From: "Bank <security@bank.example>" <someone@example.invalid>` + "\n" +
		"\nThe body.\n"
	if err := os.WriteFile(poolFile, []byte(msg), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := mailPoolFile(poolFile); err != nil {
		t.Fatalf("mailPoolFile failed: %s", err)
	}

	server.mu.Lock()
	rcpts, data := slices.Clone(server.rcpts), server.data
	server.mu.Unlock()
	slices.Sort(rcpts)
	wantRcpts := []string{"alice@example.invalid", "bob@example.invalid", "carol@example.invalid", "dave@example.invalid"}
	if !slices.Equal(rcpts, wantRcpts) {
		t.Errorf("Recipients: got %q, want %q", rcpts, wantRcpts)
	}

	delivered, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Delivered message doesn't parse: %s\n%s", err, data)
	}
	for _, h := range []string{"Bcc", "User-Agent", "Sender", "Yamn-Pooled-Date"} {
		if _, ok := delivered.Header[h]; ok {
			t.Errorf("Header %s should have been removed", h)
		}
	}
	if got := delivered.Header["To"]; len(got) != 2 {
		t.Errorf("Both To headers should be kept, got %q", got)
	}
	for _, h := range []string{"Subject", "Content-Type", "Date", "Message-Id"} {
		if delivered.Header.Get(h) == "" {
			t.Errorf("Header %s missing from delivered message", h)
		}
	}
	from, err := delivered.Header.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Address != "remailer@exit.invalid" {
		t.Errorf("From should be the outbound address, got %q (%v)", delivered.Header.Get("From"), err)
	}
	if !strings.Contains(string(data), "The body.") {
		t.Error("Body missing from delivered message")
	}
}
