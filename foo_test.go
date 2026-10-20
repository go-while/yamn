package main

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestReplyLimiter(t *testing.T) {
	l := newReplyLimiter()
	day := time.Date(2026, 10, 20, 23, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return day }

	if !l.allowed("alice@example.invalid") {
		t.Fatal("First request should be allowed")
	}
	l.record("alice@example.invalid")
	if l.allowed("alice@example.invalid") || l.allowed("ALICE@Example.invalid") {
		t.Error("Second reply to the same address the same day should be refused")
	}
	if !l.allowed("bob@example.invalid") {
		t.Error("A different address should be allowed")
	}
	day = day.Add(2 * time.Hour) // next UTC day
	if !l.allowed("alice@example.invalid") {
		t.Error("The limit should reset on a new day")
	}
}

// TestReplyLimiterKeyed checks the hash depends on the per-process key, so
// it can't be computed (or collided) from outside.
func TestReplyLimiterKeyed(t *testing.T) {
	a, b := newReplyLimiter(), newReplyLimiter()
	if a.hash("alice@example.invalid") == b.hash("alice@example.invalid") {
		t.Error("Limiters with different keys should hash an address differently")
	}
	if a.hash("alice@example.invalid") != a.hash("Alice@Example.invalid") {
		t.Error("Hash should ignore case")
	}
}

// TestRemailerFooReply sends remailer-help requests through remailerFoo to a
// fake SMTP server.
func TestRemailerFooReply(t *testing.T) {
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Help = filepath.Join(t.TempDir(), "help.txt")
	if err := os.WriteFile(cfg.Files.Help, []byte("How to use this remailer.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldLimiter := fooReplies
	fooReplies = newReplyLimiter()
	t.Cleanup(func() { fooReplies = oldLimiter })
	server := newFakeSMTP(t, nil, false, "")
	rcpts := func() []string {
		server.mu.Lock()
		defer server.mu.Unlock()
		return slices.Clone(server.rcpts)
	}

	// A From with a display name used to give an invalid SMTP recipient.
	if err := remailerFoo("remailer-help", "Alice <alice@example.invalid>"); err != nil {
		t.Fatalf("remailer-help failed: %s", err)
	}
	if got := rcpts(); !slices.Equal(got, []string{"alice@example.invalid"}) {
		t.Fatalf("Recipients: got %q, want alice's bare address", got)
	}
	// Same address again the same day: no second reply.
	if err := remailerFoo("remailer-help", "alice@example.invalid"); err == nil {
		t.Error("Second request the same day should be refused")
	}
	if n := len(rcpts()); n != 1 {
		t.Errorf("Expected 1 reply in total, got %d", n)
	}
	// Invalid From: ignored.
	if err := remailerFoo("remailer-help", "not an address"); err == nil {
		t.Error("Request with an invalid From should be refused")
	}
}

// TestRemailerFooFailedSendNotCounted checks that a reply that couldn't be
// sent doesn't use up the address's reply for the day.
func TestRemailerFooFailedSendNotCounted(t *testing.T) {
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Help = filepath.Join(t.TempDir(), "help.txt")
	if err := os.WriteFile(cfg.Files.Help, []byte("Help\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldLimiter := fooReplies
	fooReplies = newReplyLimiter()
	t.Cleanup(func() { fooReplies = oldLimiter })

	// No SMTP server: point the dial at a closed port so sending fails.
	oldDial := smtpDial
	smtpDial = func(network, _ string) (net.Conn, error) {
		return net.Dial(network, "127.0.0.1:1")
	}
	t.Cleanup(func() { smtpDial = oldDial })
	if err := remailerFoo("remailer-help", "alice@example.invalid"); err == nil {
		t.Fatal("Sending should fail without an SMTP server")
	}
	if !fooReplies.allowed("alice@example.invalid") {
		t.Error("A failed send should not count as today's reply")
	}
}
