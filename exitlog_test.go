package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	stdlog "log"

	"github.com/Masterminds/log-go"
	"github.com/luksen/maildir"
)

// logAt captures yamn's log at level lvl until the end of the test.
func logAt(t *testing.T, lvl int) *bytes.Buffer {
	t.Helper()
	buf := new(bytes.Buffer)
	oldCurrent, oldWriter := log.Current, stdlog.Writer()
	stdlog.SetOutput(buf)
	log.Current = log.StdLogger{Level: lvl}
	t.Cleanup(func() {
		log.Current = oldCurrent
		stdlog.SetOutput(oldWriter)
	})
	return buf
}

// noneIn reports which of words occur in text.
func noneIn(text string, words ...string) (found []string) {
	for _, w := range words {
		if strings.Contains(text, w) {
			found = append(found, w)
		}
	}
	return
}

// TestExitLogsNoRecipients checks that an exit's info log names neither
// the recipients nor their MX hosts, also when a server's reply quotes the
// recipient, a lookup fails or a recipient is on the destination block
// list (Y-48).  The SMTP code stays, and trace level keeps the details.
func TestExitLogsNoRecipients(t *testing.T) {
	const secret = "secret-recipient.invalid"
	run := func(lvl int) string {
		server := setupMXDelivery(t, map[string]bool{"down.invalid": true})
		server.rcptReply = map[string]string{"bob@" + secret: "550 5.1.1 <bob@" + secret + ">: no such user"}
		cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
		if err := os.WriteFile(cfg.Files.DestBlock, []byte("eve@blocked.invalid\n"), 0600); err != nil {
			t.Fatal(err)
		}
		buf := logAt(t, lvl)
		file := writePoolMessage(t, "To: bob@"+secret+", eve@blocked.invalid\nCc: carol@down.invalid, dave@ok.invalid\n")
		emailPoolFile(filepath.Base(file))
		return buf.String()
	}
	words := []string{secret, "blocked.invalid", "down.invalid", "ok.invalid", "bob@", "eve@", "carol@", "dave@"}
	info := run(log.InfoLevel)
	if found := noneIn(info, words...); len(found) > 0 {
		t.Errorf("info log names %q:\n%s", found, info)
	}
	if !strings.Contains(info, "SMTP 550") {
		t.Errorf("info log lacks the SMTP code:\n%s", info)
	}
	trace := run(log.TraceLevel)
	if !strings.Contains(trace, "bob@"+secret) || !strings.Contains(trace, "mx."+secret) {
		t.Errorf("trace log lacks the details:\n%s", trace)
	}
}

// TestRelayLogsNoRecipients checks the same for delivery through
// smtp_relay: the relay may be named (it's the operator's setting), its
// reply text and the recipients may not.
func TestRelayLogsNoRecipients(t *testing.T) {
	const secret = "secret-recipient.invalid"
	server := setupDeliveryTest(t)
	server.rcptReply = map[string]string{
		"bob@" + secret:   "450 4.2.0 <bob@" + secret + ">: try again later",
		"carol@" + secret: "550 5.1.1 <carol@" + secret + ">: no such user",
	}
	buf := logAt(t, log.InfoLevel)
	file := writePoolMessage(t, "To: bob@"+secret+", carol@"+secret+", dave@ok.invalid\n")
	emailPoolFile(filepath.Base(file))
	info := buf.String()
	if found := noneIn(info, secret, "ok.invalid", "bob@", "carol@", "dave@"); len(found) > 0 {
		t.Errorf("info log names %q:\n%s", found, info)
	}
	for _, want := range []string{"SMTP 450", "SMTP 550", "kept for a retry"} {
		if !strings.Contains(info, want) {
			t.Errorf("info log lacks %q:\n%s", want, info)
		}
	}
}

// TestRejectedMailLogsNoHeaders checks that rejecting mail that isn't a
// yamn message, often a bounce of delivered mail, doesn't log its From and
// Subject at info level.
func TestRejectedMailLogsNoHeaders(t *testing.T) {
	_, exit := setupRoundTrip(t)
	actAs(t, exit, true)
	cfg.Files.Maildir = filepath.Join(t.TempDir(), "Maildir")
	dir := maildir.Dir(cfg.Files.Maildir)
	if err := dir.Create(); err != nil {
		t.Fatal(err)
	}
	deliverMail(t, dir, []byte("From: MAILER-DAEMON@mx.secret-recipient.invalid\n"+
		"Subject: Undelivered Mail: bob@secret-recipient.invalid\n\nNo such user.\n"))
	buf := logAt(t, log.InfoLevel)
	if err := processMail(exit.secring); err != nil {
		t.Fatalf("processMail: %s", err)
	}
	info := buf.String()
	if found := noneIn(info, "secret-recipient.invalid", "MAILER-DAEMON"); len(found) > 0 {
		t.Errorf("info log names %q:\n%s", found, info)
	}
	if !strings.Contains(info, "not a yamn message") {
		t.Errorf("info log doesn't say why the mail was rejected:\n%s", info)
	}
}

// TestFooRefusalNamesNoSender checks that a refused remailer-* request
// doesn't put the requester's address in the error that gets logged.
func TestFooRefusalNamesNoSender(t *testing.T) {
	setupFooReply(t, "victim@example.invalid\n")
	if err := remailerFoo("remailer-help", "victim@example.invalid"); err == nil || strings.Contains(err.Error(), "victim") {
		t.Errorf("blocked sender: err = %v", err)
	}
	if err := remailerFoo("remailer-help", "alice@example.invalid"); err != nil {
		t.Fatal(err)
	}
	if err := remailerFoo("remailer-help", "alice@example.invalid"); err == nil || strings.Contains(err.Error(), "alice") {
		t.Errorf("second request: err = %v", err)
	}
	// A reply that can't be pooled doesn't name the request's Subject.
	cfg.Files.Pooldir = cfg.Files.Help // a file, not a directory
	if err := remailerFoo("remailer-help bob@secret.invalid", "carol@example.invalid"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("pool write failure: err = %v", err)
	}
}

// TestReceivedMailErrorsNotLogged checks that what the parsers quote from
// received mail stays out of the info log: a malformed From or an unknown
// or address-carrying Subject of a remailer-* request, a malformed armor
// line, and a malformed header line in a decrypted exit message.
func TestReceivedMailErrorsNotLogged(t *testing.T) {
	const secret = "secret-recipient.invalid"
	_, exit := setupRoundTrip(t)
	actAs(t, exit, true)
	cfg.Files.Help = filepath.Join(t.TempDir(), "help.txt")
	if err := os.WriteFile(cfg.Files.Help, []byte("Help\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
	oldLimiter := fooReplies
	fooReplies = newReplyLimiter()
	t.Cleanup(func() { fooReplies = oldLimiter })
	cfg.Files.Maildir = filepath.Join(t.TempDir(), "Maildir")
	dir := maildir.Dir(cfg.Files.Maildir)
	if err := dir.Create(); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{
		"From: Alice <bob@" + secret + "\nSubject: remailer-help\n\nx\n",
		"From: alice@example.invalid\nSubject: remailer-xyz bob@" + secret + "\n\nx\n",
		"From: carol@example.invalid\nSubject: remailer-help\n\nx\n",
		"From: carol@example.invalid\nSubject: remailer-help bob@" + secret + "\n\nx\n",
		"From: dave@example.invalid\nSubject: yamn\n\n::\n\n-----BEGIN REMAILER MESSAGE-----\nbob@" + secret + "\n",
	} {
		deliverMail(t, dir, []byte(m))
	}
	buf := logAt(t, log.InfoLevel)
	if err := processMail(exit.secring); err != nil {
		t.Fatalf("processMail: %s", err)
	}

	setupDeliveryTest(t)
	file := writePoolMessage(t, "To bob@"+secret+"\n")
	emailPoolFile(filepath.Base(file))

	info := buf.String()
	if found := noneIn(info, secret, "bob@", "remailer-xyz"); len(found) > 0 {
		t.Errorf("info log names %q:\n%s", found, info)
	}
	for _, want := range []string{"invalid From", "unknown remailer-* request", "already replied", "not a yamn message", "can't be parsed"} {
		if !strings.Contains(info, want) {
			t.Errorf("info log lacks %q:\n%s", want, info)
		}
	}
}

// TestPoolFileReasonLogged checks that a pool file dropped for one of
// mailPoolFile's own reasons says which at info level (its text holds no
// address), and that a permanent RCPT rejection isn't called temporary.
func TestPoolFileReasonLogged(t *testing.T) {
	setupDeliveryTest(t)
	cfg.Mail.MaxRecipients = 1
	buf := logAt(t, log.InfoLevel)
	file := writePoolMessage(t, "To: a@one.invalid, b@two.invalid\n")
	emailPoolFile(filepath.Base(file))
	info := buf.String()
	for _, want := range []string{"dropped", "exceed max_recipients"} {
		if !strings.Contains(info, want) {
			t.Errorf("info log lacks %q:\n%s", want, info)
		}
	}
	if found := noneIn(info, "one.invalid", "two.invalid", "temporary"); len(found) > 0 {
		t.Errorf("info log has %q:\n%s", found, info)
	}
}
