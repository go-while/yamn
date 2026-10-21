package main

import (
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// setupTarpitTest gives the test an empty pool, a help file and a fresh
// reply limiter, and makes SMTP sessions time out after timeout.
func setupTarpitTest(t *testing.T, timeout time.Duration) {
	t.Helper()
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Pooldir = t.TempDir()
	cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
	cfg.Files.Help = filepath.Join(t.TempDir(), "help.txt")
	if err := os.WriteFile(cfg.Files.Help, []byte("Help\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldLimiter := fooReplies
	fooReplies = newReplyLimiter()
	t.Cleanup(func() { fooReplies = oldLimiter })
	oldTimeout := smtpTimeout
	smtpTimeout = timeout
	t.Cleanup(func() { smtpTimeout = oldTimeout })
}

// TestRemailerFooDoesNotBlock checks that a remailer-* request whose reply
// goes to a mail server that never answers doesn't hold up the caller (the
// loop that reads incoming mail), and that the request counts even though
// the reply wasn't sent, so it can't be repeated.
func TestRemailerFooDoesNotBlock(t *testing.T) {
	setupTarpitTest(t, 2*time.Second)
	newFakeSMTP(t, nil, false, "greeting")
	start := time.Now()
	remailerFoo("remailer-help", "victim@tarpit.example")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("remailerFoo blocked for %s on a server that never answers", elapsed.Round(time.Millisecond))
	}
	if fooReplies.allowed("victim@tarpit.example") {
		t.Error("the request should count as today's reply before it's sent")
	}
}

// TestPoolSendConcurrent checks that a mail server that never answers holds
// up only the pool file sent to it: another file is delivered meanwhile.
func TestPoolSendConcurrent(t *testing.T) {
	setupTarpitTest(t, 2*time.Second)
	stalled := newFakeSMTP(t, nil, false, "greeting")
	good := newFakeSMTP(t, nil, false, "")
	// The first connection goes to the stalled server, the others to the
	// good one.
	var dials atomic.Int32
	smtpDial = func(network, _ string) (net.Conn, error) {
		if dials.Add(1) == 1 {
			return net.Dial(network, stalled.ln.Addr().String())
		}
		return net.Dial(network, good.ln.Addr().String())
	}
	writePoolMessage(t, "To: a@example.invalid\n")
	writePoolMessage(t, "To: b@example.invalid\n")
	done := make(chan struct{})
	go func() {
		poolOutboundSend()
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, _, delivered := good.result(); delivered {
			break
		}
		if time.Now().After(deadline) {
			t.Error("the second file waited for the server that never answers")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	<-done
}
