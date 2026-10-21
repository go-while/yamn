package main

import (
	"bytes"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestMessageIDDomain(t *testing.T) {
	setupTestConfig(t)
	cfg.Remailer.Address = "mix@remailer.invalid"
	for _, tt := range []struct{ messageDomain, want string }{
		{"", "remailer.invalid"}, // default: remailer.address
		{"mail.example.invalid", "mail.example.invalid"},
		{"bad domain", "remailer.invalid"},     // invalid: ignored
		{"x>\r\nBcc: a@b", "remailer.invalid"}, // header-breaking: ignored
		{"a@b", "remailer.invalid"},
	} {
		cfg.Mail.MessageDomain = tt.messageDomain
		id := messageID()
		if !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@"+tt.want+">") {
			t.Errorf("message_domain %q: got %q, want a Message-ID ending in @%s>", tt.messageDomain, id, tt.want)
		}
	}
}

func TestNagInvalidMessageDomain(t *testing.T) {
	setupTestConfig(t)
	logged := captureLog(t)
	cfg.Mail.MessageDomain = "good.example.invalid"
	nagOperator()
	if strings.Contains(logged.String(), "message_domain") {
		t.Errorf("A valid message_domain shouldn't be reported, got:\n%s", logged)
	}
	cfg.Mail.MessageDomain = "not valid"
	nagOperator()
	if !strings.Contains(logged.String(), "message_domain") {
		t.Errorf("An invalid message_domain should be reported, got:\n%s", logged)
	}
}

// TestMailDatesUTC checks that the Date and Message-ID an exit sets don't
// show the remailer's time zone.
func TestMailDatesUTC(t *testing.T) {
	setupTestConfig(t)
	oldLocal := time.Local
	time.Local = time.FixedZone("Test", 5*3600+30*60)
	t.Cleanup(func() { time.Local = oldLocal })
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Pooldir = t.TempDir()
	server := newFakeSMTP(t, nil, false, "")
	file := writePoolMessage(t, "To: alice@example.invalid\n")
	if _, err := mailPoolFile(file); err != nil {
		t.Fatalf("mailPoolFile failed: %s", err)
	}
	server.mu.Lock()
	data := server.data
	server.mu.Unlock()
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if date := msg.Header.Get("Date"); !strings.HasSuffix(date, " +0000") {
		t.Errorf("Date %q should be UTC", date)
	}
	id := strings.TrimPrefix(msg.Header.Get("Message-Id"), "<")
	stamp, err := time.Parse("20060102.150405", id[:min(len(id), 15)])
	if err != nil || time.Since(stamp).Abs() > time.Minute {
		t.Errorf("Message-ID %q should start with the UTC time", id)
	}
}
