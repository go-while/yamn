package main

import (
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// setupFooReply sets up remailerFoo with a help file, an empty reply limit
// and a destination block list holding block.
func setupFooReply(t *testing.T, block string) *fakeSMTP {
	t.Helper()
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Pooldir = t.TempDir()
	cfg.Files.Help = filepath.Join(t.TempDir(), "help.txt")
	if err := os.WriteFile(cfg.Files.Help, []byte("Help\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
	if err := os.WriteFile(cfg.Files.DestBlock, []byte(block), 0600); err != nil {
		t.Fatal(err)
	}
	oldLimiter := fooReplies
	fooReplies = newReplyLimiter()
	t.Cleanup(func() { fooReplies = oldLimiter })
	return newFakeSMTP(t, nil, false, "")
}

// TestRemailerFooDestBlock checks that remailer-* replies honour the
// destination block list, and fail closed if it can't be read.
func TestRemailerFooDestBlock(t *testing.T) {
	server := setupFooReply(t, "victim@example.invalid\n@blocked.invalid\n")
	for _, from := range []string{"victim@example.invalid", "Anyone <anyone@blocked.invalid>"} {
		if err := remailerFoo("remailer-help", from); err == nil {
			t.Errorf("Request from blocked %q should be refused", from)
		}
	}
	cfg.Files.DestBlock = t.TempDir() // a directory: reading it fails
	if err := remailerFoo("remailer-help", "carol@example.invalid"); err == nil {
		t.Error("Request should be refused when the block list can't be read")
	}
	server.mu.Lock()
	rcpts := slices.Clone(server.rcpts)
	server.mu.Unlock()
	if len(rcpts) != 0 {
		t.Errorf("Replies sent to %q", rcpts)
	}
	if !fooReplies.allowed("carol@example.invalid") {
		t.Error("A refused reply should not count as today's reply")
	}
}

// TestRemailerFooReplyHeaders checks that remailer-* replies carry Date,
// Message-ID and MIME headers.  They used to have only From, To and
// Subject, which some MTAs reject or score as spam.
func TestRemailerFooReplyHeaders(t *testing.T) {
	server := setupFooReply(t, "")
	if err := remailerFoo("remailer-help", "someone@example.invalid"); err != nil {
		t.Fatalf("remailer-help failed: %s", err)
	}
	// Replies are pooled and sent with the pool.
	poolOutboundSend()
	server.mu.Lock()
	data := string(server.data)
	server.mu.Unlock()
	m, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Header.Date(); err != nil {
		t.Errorf("Date: %s", err)
	}
	if id := m.Header.Get("Message-Id"); !strings.HasPrefix(id, "<") || !strings.Contains(id, "@") {
		t.Errorf("Message-ID: got %q", id)
	}
	if v := m.Header.Get("Mime-Version"); v != "1.0" {
		t.Errorf("MIME-Version: got %q", v)
	}
	if ct := m.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type: got %q", ct)
	}
}
