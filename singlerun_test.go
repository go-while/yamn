package main

import (
	"slices"
	"testing"
)

// setupSingleRun prepares a remailer for one loopServer pass without the
// daemon, with a remailer-* reply waiting in the pool.
func setupSingleRun(t *testing.T) *fakeSMTP {
	t.Helper()
	server := setupFooReply(t, "")
	cfg.Urls.Fetch = false
	cfg.Remailer.Daemon = false
	flag.Remailer = true
	flag.Daemon = false
	if err := remailerFoo("remailer-help", "someone@example.invalid"); err != nil {
		t.Fatalf("remailer-help: %s", err)
	}
	return server
}

func sentTo(s *fakeSMTP) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.rcpts)
}

// TestSingleRunSendsPool checks that "yamn -M" without -D sends from the
// pool, as yamn(1) says ("a single iteration of read, process, send").
// It used to read and process only, so pooled mail and remailer-* replies
// waited until a daemon or -S ran.
func TestSingleRunSendsPool(t *testing.T) {
	server := setupSingleRun(t)
	if err := loopServer(); err != nil {
		t.Fatalf("loopServer: %s", err)
	}
	if got := sentTo(server); !slices.Contains(got, "someone@example.invalid") {
		t.Fatalf("sent to %q, want the remailer-help reply to someone@example.invalid", got)
	}
}

// TestSingleRunWithFlushLeavesPool checks that "yamn -M -S" leaves the
// sending to the flush that follows the server pass.
func TestSingleRunWithFlushLeavesPool(t *testing.T) {
	server := setupSingleRun(t)
	flag.Send = true
	if err := loopServer(); err != nil {
		t.Fatalf("loopServer: %s", err)
	}
	if got := sentTo(server); len(got) != 0 {
		t.Fatalf("loopServer sent to %q; with -S the flush sends", got)
	}
}
