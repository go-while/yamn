package main

import (
	"bytes"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const identifyingMessage = "From: John Smith <john@real.invalid>\n" +
	"Reply-To: john@real.invalid\n" +
	"To: alice@example.invalid\n" +
	"Bcc: bob@example.invalid\n" +
	"Subject: hello\n" +
	"Date: Tue, 20 Oct 2026 14:00:00 +0200\n" +
	"Message-ID: <123@johns-laptop.local>\n" +
	"User-Agent: Leaky/1.0\n" +
	"X-Mailer: Leaky\n" +
	"Content-Type: text/plain\n" +
	"\nThe body.\n"

func TestStripClientHeaders(t *testing.T) {
	setupTestConfig(t)
	msg, err := mail.ReadMessage(strings.NewReader(identifyingMessage))
	if err != nil {
		t.Fatal(err)
	}
	removed := stripClientHeaders(msg.Header, false, false)
	wantRemoved := []string{"Date", "From", "Message-Id", "Reply-To", "User-Agent", "X-Mailer"}
	if !slices.Equal(removed, wantRemoved) {
		t.Errorf("Removed: got %q, want %q", removed, wantRemoved)
	}
	var kept []string
	for k := range msg.Header {
		kept = append(kept, k)
	}
	slices.Sort(kept)
	wantKept := []string{"Bcc", "Content-Type", "Subject", "To"}
	if !slices.Equal(kept, wantKept) {
		t.Errorf("Kept: got %q, want %q", kept, wantKept)
	}
}

// TestStripClientHeadersKeepFrom checks that --keep-from and --keep-reply-to
// each keep their header unchanged, and nothing else.
func TestStripClientHeadersKeepFrom(t *testing.T) {
	setupTestConfig(t)
	for _, tc := range []struct {
		keepFrom, keepReplyTo bool
		from, replyTo         string
	}{
		{true, false, "John Smith <john@real.invalid>", ""},
		{false, true, "", "john@real.invalid"},
		{true, true, "John Smith <john@real.invalid>", "john@real.invalid"},
	} {
		msg, err := mail.ReadMessage(strings.NewReader(identifyingMessage))
		if err != nil {
			t.Fatal(err)
		}
		removed := stripClientHeaders(msg.Header, tc.keepFrom, tc.keepReplyTo)
		if got := msg.Header.Get("From"); got != tc.from {
			t.Errorf("keepFrom=%v: From got %q, want %q", tc.keepFrom, got, tc.from)
		}
		if got := msg.Header.Get("Reply-To"); got != tc.replyTo {
			t.Errorf("keepReplyTo=%v: Reply-To got %q, want %q", tc.keepReplyTo, got, tc.replyTo)
		}
		if !slices.Contains(removed, "User-Agent") {
			t.Errorf("User-Agent should still be removed, removed %q", removed)
		}
	}
}

// sendThroughExit runs the real client on a message and decodes the result
// at the exit.  It returns the plaintext the exit pools for delivery and the
// notices the client printed.
// setFlags, if not nil, sets the --keep-* flags.
func sendThroughExit(t *testing.T, message string, setFlags func()) (delivered []byte, notices string) {
	t.Helper()
	_, exit := setupRoundTrip(t)
	cfg.Urls.Fetch = false
	flag.Chain = exit.address
	flag.Copies = 1
	if setFlags != nil {
		setFlags()
	}
	out := new(bytes.Buffer)
	oldOutput := clientOutput
	clientOutput = out
	t.Cleanup(func() { clientOutput = oldOutput })

	msgFile := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(msgFile, []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	flag.Args = []string{msgFile}
	clientPool := t.TempDir()
	cfg.Files.Pooldir = clientPool
	mixprep()
	exitPool := deliverPool(t, clientPool, exit, true)
	files := poolFiles(t, exitPool, "m")
	if len(files) != 1 {
		t.Fatalf("Exit should pool 1 message, got %d", len(files))
	}
	return files[0], out.String()
}

func TestMixprepStripsHeaders(t *testing.T) {
	delivered, notices := sendThroughExit(t, identifyingMessage, nil)
	// The exit's pool file starts with its own Yamn-Pooled-Date header,
	// followed by the client's message.
	msg, err := mail.ReadMessage(bytes.NewReader(delivered))
	if err != nil {
		t.Fatalf("Delivered message doesn't parse: %s", err)
	}
	for _, h := range []string{"User-Agent", "X-Mailer", "Date", "Message-Id", "From", "Reply-To"} {
		if v := msg.Header.Get(h); v != "" {
			t.Errorf("%s should have been removed by the client, got %q", h, v)
		}
	}
	if bytes.Contains(delivered, []byte("real.invalid")) || bytes.Contains(delivered, []byte("John Smith")) {
		t.Errorf("The sender's name or address reached the exit:\n%s", delivered)
	}
	for h, want := range map[string]string{
		"Bcc":     "bob@example.invalid",
		"Subject": "hello",
	} {
		if got := msg.Header.Get(h); got != want {
			t.Errorf("%s: got %q, want %q", h, got, want)
		}
	}
	if !bytes.Contains(delivered, []byte("The body.")) {
		t.Error("Body missing from the message the exit received")
	}
	for _, want := range []string{"From", "Reply-To", "User-Agent", "--keep-headers", "--keep-from", "--keep-reply-to"} {
		if !strings.Contains(notices, want) {
			t.Errorf("The client's notice should mention %q, got %q", want, notices)
		}
	}
}

// TestMixprepKeepFrom sends a pseudonym From with --keep-from: the exit
// receives it unchanged, while Reply-To and the other headers are removed.
func TestMixprepKeepFrom(t *testing.T) {
	delivered, notices := sendThroughExit(t, identifyingMessage, func() { flag.KeepFrom = true })
	msg, err := mail.ReadMessage(bytes.NewReader(delivered))
	if err != nil {
		t.Fatalf("Delivered message doesn't parse: %s", err)
	}
	if got := msg.Header.Get("From"); got != "John Smith <john@real.invalid>" {
		t.Errorf("From: got %q, want it unchanged", got)
	}
	for _, h := range []string{"Reply-To", "User-Agent"} {
		if v := msg.Header.Get(h); v != "" {
			t.Errorf("%s should have been removed, got %q", h, v)
		}
	}
	if strings.Contains(notices, "From,") || !strings.Contains(notices, "Reply-To") {
		t.Errorf("Notice should list Reply-To but not From, got %q", notices)
	}
}

func TestMixprepKeepHeaders(t *testing.T) {
	delivered, notices := sendThroughExit(t, identifyingMessage, func() { flag.KeepHeaders = true })
	for _, h := range []string{"User-Agent: Leaky/1.0", "John Smith", "Reply-To: john@real.invalid"} {
		if !bytes.Contains(delivered, []byte(h)) {
			t.Errorf("%q should be kept with --keep-headers", h)
		}
	}
	if notices != "" {
		t.Errorf("Nothing removed, so nothing to report; got %q", notices)
	}
}

// TestReadMessageStdin checks that a message from stdin gets the same
// treatment as one from a file: --to, --subject and header stripping.
func TestReadMessageStdin(t *testing.T) {
	setupTestConfig(t)
	flag.To = "carol@example.invalid"
	flag.Subject = "from flags"
	oldOutput := clientOutput
	clientOutput = new(bytes.Buffer)
	t.Cleanup(func() { clientOutput = oldOutput })
	plain := readMessage(strings.NewReader("To: alice@example.invalid\nUser-Agent: Leaky\n\nbody\n"), "stdin")
	msg, err := mail.ReadMessage(bytes.NewReader(plain))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("To") != "carol@example.invalid" || msg.Header.Get("Subject") != "from flags" {
		t.Errorf("--to/--subject not applied: To=%q Subject=%q", msg.Header.Get("To"), msg.Header.Get("Subject"))
	}
	if msg.Header.Get("User-Agent") != "" {
		t.Error("User-Agent should be removed from stdin messages too")
	}
}

// TestMixprepStdinStripsHeaders runs the client with the message on stdin.
func TestMixprepStdinStripsHeaders(t *testing.T) {
	_, exit := setupRoundTrip(t)
	cfg.Urls.Fetch = false
	flag.Chain = exit.address
	flag.Copies = 1
	flag.Args = nil
	oldOutput := clientOutput
	clientOutput = new(bytes.Buffer)
	t.Cleanup(func() { clientOutput = oldOutput })
	stdinFile := filepath.Join(t.TempDir(), "stdin.txt")
	if err := os.WriteFile(stdinFile, []byte(identifyingMessage), 0600); err != nil {
		t.Fatal(err)
	}
	stdin, err := os.Open(stdinFile)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	oldStdin := os.Stdin
	os.Stdin = stdin
	t.Cleanup(func() { os.Stdin = oldStdin })

	clientPool := t.TempDir()
	cfg.Files.Pooldir = clientPool
	mixprep()
	files := poolFiles(t, deliverPool(t, clientPool, exit, true), "m")
	if len(files) != 1 || bytes.Contains(files[0], []byte("User-Agent")) {
		t.Fatalf("Expected 1 delivered message without User-Agent, got %d messages", len(files))
	}
}
