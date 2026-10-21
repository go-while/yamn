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

func setupBareCR(t *testing.T) *fakeSMTP {
	t.Helper()
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Pooldir = t.TempDir()
	return newFakeSMTP(t, nil, false, "")
}

// TestMailPoolFileBareCRBody checks that a lone CR in the body goes out as a
// line end.  "\r.\r" used to reach the SMTP server unescaped.
func TestMailPoolFileBareCRBody(t *testing.T) {
	server := setupBareCR(t)
	file := filepath.Join(cfg.Files.Pooldir, "mbarecr")
	msg := "Yamn-Pooled-Date: " + time.Now().Format(shortdate) +
		"\nTo: a@example.invalid\nSubject: x\n\nline1\r.\rline2\r\nline3\r"
	if err := os.WriteFile(file, []byte(msg), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := mailPoolFile(file); err != nil {
		t.Fatalf("mailPoolFile failed: %s", err)
	}
	server.mu.Lock()
	data := server.data
	server.mu.Unlock()
	// The fake server turns CRLF into LF and removes dot-stuffing, so any
	// CR left is a lone one.
	if bytes.Contains(data, []byte("\r")) {
		t.Errorf("A lone CR reached the server: %q", data)
	}
	if !bytes.HasSuffix(data, []byte("\n\nline1\n.\nline2\nline3\n")) {
		t.Errorf("Body: got %q", data)
	}
}

// TestMailPoolFileBareCRHeader checks that a header value with a CR or NUL
// in it isn't mailed: a receiver that takes a lone CR as a line end would
// see a second header the exit never filtered.
func TestMailPoolFileBareCRHeader(t *testing.T) {
	for _, header := range []string{
		"Reply-To: x@example.invalid\rFrom: ceo@bank.example\n",
		"Subject: a\x00b\n",
	} {
		server := setupBareCR(t)
		file := writePoolMessage(t, "To: a@example.invalid\n"+header)
		delFlag, err := mailPoolFile(file)
		if err == nil || !delFlag {
			t.Errorf("%q: expected an error and deletion, got err=%v delFlag=%v", header, err, delFlag)
		}
		if _, _, delivered := server.result(); delivered {
			t.Errorf("%q: message was delivered", header)
		}
	}
}

// TestStripClientHeadersBareCR checks that the client removes headers with a
// CR or NUL in them and turns a lone CR in the body into a line end.
func TestStripClientHeadersBareCR(t *testing.T) {
	setupTestConfig(t)
	msg, err := mail.ReadMessage(strings.NewReader(
		"To: a@example.invalid\nReply-To: x@example.invalid\rFrom: ceo@bank.example\n" +
			"Subject: a\x00b\n\nline1\r.\rline2\n"))
	if err != nil {
		t.Fatal(err)
	}
	removed := stripClientHeaders(msg.Header)
	want := []string{"Reply-To (CR or NUL)", "Subject (CR or NUL)"}
	if !slices.Equal(removed, want) {
		t.Errorf("Removed: got %q, want %q", removed, want)
	}
	if badHeader(msg.Header) != "" {
		t.Errorf("Headers left with a CR or NUL: %q", msg.Header)
	}
	if got := assemble(*msg); !bytes.HasSuffix(got, []byte("\n\nline1\n.\nline2\n")) {
		t.Errorf("Assembled message: got %q", got)
	}
}
