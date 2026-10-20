package main

import (
	"strings"
	"testing"
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
