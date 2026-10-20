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
	removed := stripClientHeaders(msg.Header)
	wantRemoved := []string{"Date", "From display name", "Message-Id", "User-Agent", "X-Mailer"}
	if !slices.Equal(removed, wantRemoved) {
		t.Errorf("Removed: got %q, want %q", removed, wantRemoved)
	}
	var kept []string
	for k := range msg.Header {
		kept = append(kept, k)
	}
	slices.Sort(kept)
	wantKept := []string{"Bcc", "Content-Type", "From", "Subject", "To"}
	if !slices.Equal(kept, wantKept) {
		t.Errorf("Kept: got %q, want %q", kept, wantKept)
	}
	if from := msg.Header.Get("From"); from != "john@real.invalid" {
		t.Errorf("From: got %q, want the bare address", from)
	}
}

// sendThroughExit runs the real client on a message and decodes the result
// at the exit.  It returns the plaintext the exit pools for delivery and the
// notices the client printed.
func sendThroughExit(t *testing.T, message string, keepHeaders bool) (delivered []byte, notices string) {
	t.Helper()
	_, exit := setupRoundTrip(t)
	cfg.Urls.Fetch = false
	flag.Chain = exit.address
	flag.Copies = 1
	flag.KeepHeaders = keepHeaders
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
	delivered, notices := sendThroughExit(t, identifyingMessage, false)
	// The exit's pool file starts with its own Yamn-Pooled-Date header,
	// followed by the client's message.
	msg, err := mail.ReadMessage(bytes.NewReader(delivered))
	if err != nil {
		t.Fatalf("Delivered message doesn't parse: %s", err)
	}
	for _, h := range []string{"User-Agent", "X-Mailer", "Date", "Message-Id"} {
		if v := msg.Header.Get(h); v != "" {
			t.Errorf("%s should have been removed by the client, got %q", h, v)
		}
	}
	for h, want := range map[string]string{
		"From":    "john@real.invalid",
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
	if !strings.Contains(notices, "User-Agent") || !strings.Contains(notices, "--keep-headers") {
		t.Errorf("The client should report what it removed, got %q", notices)
	}
}

func TestMixprepKeepHeaders(t *testing.T) {
	delivered, notices := sendThroughExit(t, identifyingMessage, true)
	for _, h := range []string{"User-Agent: Leaky/1.0", "John Smith"} {
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
