package main

import (
	"bytes"
	"net/mail"
	"os"
	"path/filepath"
	"testing"

	"github.com/luksen/maildir"
)

func TestMboxRoundTrip(t *testing.T) {
	mails := [][]byte{
		[]byte("To: a@example.invalid\n\nFrom the start, a line that looks like a separator.\n>From an already quoted line.\n"),
		[]byte("To: b@example.invalid\n\nSecond mail.\n"),
	}
	var mbox bytes.Buffer
	for _, m := range mails {
		if err := writeMbox(&mbox, m); err != nil {
			t.Fatal(err)
		}
	}
	if !isMbox(mbox.Bytes()) {
		t.Fatal("writeMbox output should be recognised as an mbox")
	}
	got := splitMbox(mbox.Bytes())
	if len(got) != len(mails) {
		t.Fatalf("Expected %d mails, got %d:\n%s", len(mails), len(got), mbox.String())
	}
	for i := range mails {
		if !bytes.Equal(got[i], mails[i]) {
			t.Errorf("Mail %d changed:\ngot  %q\nwant %q", i, got[i], mails[i])
		}
	}
}

func TestIsMbox(t *testing.T) {
	if isMbox([]byte("From: someone@example.invalid\nTo: x@y\n\nbody\n")) {
		t.Error("A mail starting with a From: header is not an mbox")
	}
	if !isMbox([]byte("From yamn Tue Oct 20 21:00:00 2026\nTo: x@y\n\nbody\n\n")) {
		t.Error("An mbox separator line should be recognised")
	}
}

// clientStdout runs the real client with --stdout and 2 copies, and returns
// what it printed.
func clientStdout(t *testing.T, exit *testRemailer) []byte {
	t.Helper()
	cfg.Urls.Fetch = false
	flag.Chain = exit.address
	flag.Copies = 2
	flag.Stdout = true
	out := new(bytes.Buffer)
	oldOutput := mailOutput
	mailOutput = out
	t.Cleanup(func() { mailOutput = oldOutput })
	msgFile := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(msgFile, []byte("To: alice@example.invalid\n\nHello via stdout\n"), 0600); err != nil {
		t.Fatal(err)
	}
	flag.Args = []string{msgFile}
	cfg.Files.Pooldir = t.TempDir()
	mixprep()
	if n := len(poolFiles(t, cfg.Files.Pooldir, "")); n != 0 {
		t.Fatalf("--stdout should not write to the pool, found %d files", n)
	}
	return out.Bytes()
}

func TestMixprepStdout(t *testing.T) {
	_, exit := setupRoundTrip(t)
	out := clientStdout(t, exit)
	mails := splitMbox(out)
	if len(mails) != 2 {
		t.Fatalf("Expected 2 mails (copies) on stdout, got %d:\n%.300s", len(mails), out)
	}
	for i, m := range mails {
		msg, err := mail.ReadMessage(bytes.NewReader(m))
		if err != nil {
			t.Fatalf("Mail %d doesn't parse: %s", i, err)
		}
		if to := msg.Header.Get("To"); to != exit.address {
			t.Errorf("Mail %d addressed to %q, want the first hop %s", i, to, exit.address)
		}
		if _, err := stripArmor(bytes.NewReader(m)); err != nil {
			t.Errorf("Mail %d has no valid yamn armor: %s", i, err)
		}
	}
}

// TestStdoutIntoReadMail feeds --stdout output to -R (deliverToMaildir) at
// the exit remailer, which then decodes and delivers the message once.
func TestStdoutIntoReadMail(t *testing.T) {
	_, exit := setupRoundTrip(t)
	out := clientStdout(t, exit)

	pool := actAs(t, exit, true)
	cfg.Files.Maildir = filepath.Join(t.TempDir(), "Maildir")
	dir := maildir.Dir(cfg.Files.Maildir)
	if err := dir.Create(); err != nil {
		t.Fatal(err)
	}
	n, err := deliverToMaildir(dir, out)
	if err != nil || n != 2 {
		t.Fatalf("deliverToMaildir: delivered %d, err %v; want 2 mails", n, err)
	}
	if err := processMail(exit.secring); err != nil {
		t.Fatalf("processMail failed: %s", err)
	}
	delivered := poolFiles(t, pool, "m")
	if len(delivered) != 1 || !bytes.Contains(delivered[0], []byte("Hello via stdout")) {
		t.Fatalf("Exit should deliver the message once, got %d messages", len(delivered))
	}
}

func TestReadMailSingle(t *testing.T) {
	setupTestConfig(t)
	cfg.Files.Maildir = filepath.Join(t.TempDir(), "Maildir")
	dir := maildir.Dir(cfg.Files.Maildir)
	if err := dir.Create(); err != nil {
		t.Fatal(err)
	}
	single := []byte("From: someone@example.invalid\nTo: x@example.invalid\n\nbody\n")
	if n, err := deliverToMaildir(dir, single); err != nil || n != 1 {
		t.Fatalf("deliverToMaildir: delivered %d, err %v; want 1", n, err)
	}
	files := dirFiles(t, filepath.Join(cfg.Files.Maildir, "new"))
	if len(files) != 1 || files[0] != string(single) {
		t.Fatalf("A single mail should be stored unchanged, got %q", files)
	}
}
