package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recipients returns n distinct addresses.
func recipients(n int) string {
	var to []string
	for i := 0; i < n; i++ {
		to = append(to, fmt.Sprintf("r%d@example.invalid", i))
	}
	return strings.Join(to, ", ")
}

// TestMailPoolFileMaxRecipients checks mail.max_recipients.  One message
// used to go to any number of recipients, which made an exit a cheap spam
// amplifier.
func TestMailPoolFileMaxRecipients(t *testing.T) {
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Pooldir = t.TempDir()
	cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
	if err := os.WriteFile(cfg.Files.DestBlock, []byte("blocked@example.invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg.Mail.MaxRecipients != 10 {
		t.Fatalf("Default max_recipients: got %d, want 10", cfg.Mail.MaxRecipients)
	}
	for _, tc := range []struct {
		name    string
		limit   int
		headers string
		want    int // RCPT commands, 0 = refused
	}{
		{"at the limit", 10, "To: " + recipients(6) + "\nBcc: r6@example.invalid, r7@example.invalid, r8@example.invalid, r9@example.invalid\n", 10},
		{"over the limit", 10, "To: " + recipients(8) + "\nCc: x@example.invalid\nBcc: y@example.invalid, z@example.invalid\n", 0},
		{"blocked ones don't count", 10, "To: " + recipients(10) + ", blocked@example.invalid\n", 10},
		{"duplicates don't count", 10, "To: " + recipients(10) + "\nCc: r0@example.invalid\n", 10},
		{"no limit", 0, "To: " + recipients(50) + "\n", 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg.Mail.MaxRecipients = tc.limit
			server := newFakeSMTP(t, nil, false, "")
			file := writePoolMessage(t, tc.headers)
			delFlag, err := mailPoolFile(file)
			server.mu.Lock()
			n := len(server.rcpts)
			server.mu.Unlock()
			if tc.want == 0 {
				if err == nil || !delFlag {
					t.Errorf("Expected an error and deletion, got err=%v delFlag=%v", err, delFlag)
				}
				if n != 0 {
					t.Errorf("Message over the limit sent to %d recipients", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("mailPoolFile failed: %s", err)
			}
			if n != tc.want {
				t.Errorf("RCPT commands: got %d, want %d", n, tc.want)
			}
		})
	}
}

// TestRemailerConfMaxRecipients checks that remailer-conf announces the
// limit.
func TestRemailerConfMaxRecipients(t *testing.T) {
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	oldLimiter := fooReplies
	t.Cleanup(func() { fooReplies = oldLimiter })
	for limit, want := range map[int]string{10: "Maximum recipients: 10\n", 0: "Maximum recipients: unlimited\n"} {
		cfg.Mail.MaxRecipients = limit
		fooReplies = newReplyLimiter()
		server := newFakeSMTP(t, nil, false, "")
		if err := remailerFoo("remailer-conf", "someone@example.invalid"); err != nil {
			t.Fatalf("remailer-conf failed: %s", err)
		}
		server.mu.Lock()
		data := string(server.data)
		server.mu.Unlock()
		if !strings.Contains(data, want) {
			t.Errorf("max_recipients %d: reply lacks %q:\n%s", limit, want, data)
		}
	}
}
