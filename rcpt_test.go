package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestValidRecipient(t *testing.T) {
	for addy, want := range map[string]bool{
		"alice@example.invalid":            true,
		"Alice.B+tag@Mail-1.example.org":   true,
		"o'brien@example.invalid":          true,
		"root@localhost":                   true,
		"":                                 false,
		"alice":                            false,
		"@example.invalid":                 false,
		"alice@":                           false,
		"a@b@example.invalid":              false,
		".alice@example.invalid":           false,
		"alice.@example.invalid":           false,
		"al..ice@example.invalid":          false,
		"victim@blocked.example> NOTIFY=x": false,
		"a b@example.invalid":              false,
		"a<b@example.invalid":              false,
		"a\"b@example.invalid":             false,
		"a\rb@example.invalid":             false,
		"a\x00b@example.invalid":           false,
		"victim%blocked.example@relay.ex":  false,
		"relay!victim@example.invalid":     false,
		"victim@[192.0.2.1]":               false,
		"victim@example.org.":              false,
		"victim@-example.org":              false,
		"victim@exa_mple.org":              false,
		"jörg@example.invalid":             false,
	} {
		if got := validRecipient(addy); got != want {
			t.Errorf("validRecipient(%q) = %v, want %v", addy, got, want)
		}
	}
}

// TestMailPoolFileQuotedRecipient checks that a quoted local part can't
// carry ESMTP parameters into RCPT TO and past the block list.
// mail.ParseAddress returns it unquoted, so it used to go out as
// "RCPT TO:<victim@blocked.example> NOTIFY=NEVER ...@b.example>".
func TestMailPoolFileQuotedRecipient(t *testing.T) {
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Pooldir = t.TempDir()
	cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
	if err := os.WriteFile(cfg.Files.DestBlock, []byte("@blocked.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const injected = `"victim@blocked.example> NOTIFY=NEVER ORCPT=rfc822;x"@b.example`

	t.Run("invalid recipient skipped", func(t *testing.T) {
		server := newFakeSMTP(t, nil, false, "")
		file := writePoolMessage(t, "To: "+injected+"\nCc: carol@example.invalid, \"x@y\"@relay.example\n")
		if _, err := mailPoolFile(file); err != nil {
			t.Fatalf("mailPoolFile failed: %s", err)
		}
		server.mu.Lock()
		rcpts := slices.Clone(server.rcpts)
		server.mu.Unlock()
		if !slices.Equal(rcpts, []string{"carol@example.invalid"}) {
			t.Errorf("Recipients: got %q, want only carol", rcpts)
		}
	})
	t.Run("no valid recipient", func(t *testing.T) {
		server := newFakeSMTP(t, nil, false, "")
		file := writePoolMessage(t, "To: "+injected+"\n")
		delFlag, err := mailPoolFile(file)
		if err == nil || !delFlag {
			t.Fatalf("Expected an error and deletion, got err=%v delFlag=%v", err, delFlag)
		}
		if _, _, delivered := server.result(); delivered {
			t.Error("Message without a valid recipient was delivered")
		}
	})
}

// TestRemailerFooQuotedFrom checks that a remailer-* request from a quoted
// local part gets no reply.
func TestRemailerFooQuotedFrom(t *testing.T) {
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
	server := newFakeSMTP(t, nil, false, "")
	if err := remailerFoo("remailer-help", `"victim@example.invalid> NOTIFY=NEVER"@b.example`); err == nil {
		t.Error("Request from a quoted local part should be refused")
	}
	server.mu.Lock()
	rcpts := slices.Clone(server.rcpts)
	server.mu.Unlock()
	if len(rcpts) != 0 {
		t.Errorf("Reply sent to %q", rcpts)
	}
}
