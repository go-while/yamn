package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/luksen/maildir"
)

// setupSingleRun prepares a remailer for one loopServer pass without the
// daemon, with a remailer-* reply waiting in the pool.
func setupSingleRun(t *testing.T) *fakeSMTP {
	t.Helper()
	server := setupFooReply(t, "")
	// loopServer sets these globals (and closes both databases when it
	// returns); put them back.
	oldPub, oldID, oldChunk := Pubring, IDDb, ChunkDb
	t.Cleanup(func() {
		if ChunkDb != nil && ChunkDb != oldChunk {
			ChunkDb.Close()
		}
		Pubring, IDDb, ChunkDb = oldPub, oldID, oldChunk
	})
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

// TestSingleRunDeliversPooledMail checks that the single run sends pooled
// mail (pool/m*) too: delivered, and gone from the pool.
func TestSingleRunDeliversPooledMail(t *testing.T) {
	server := setupSingleRun(t)
	cfg.Pool.Size = 0
	cfg.Pool.MinSend = 1
	cfg.Pool.Rate = 100
	file := writePoolMessage(t, "To: alice@example.invalid\n")
	if err := loopServer(); err != nil {
		t.Fatalf("loopServer: %s", err)
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("the pool file is still there")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	delivered := false
	for _, s := range server.sessions {
		if slices.Contains(s, "alice@example.invalid") {
			delivered = true
		}
	}
	if !delivered {
		t.Errorf("sessions %q, want a delivery to alice@example.invalid", server.sessions)
	}
}

// TestSingleRunReplyLimitAcrossRuns checks that two single remailer runs,
// as from cron, reply to the same address only once a day: the second run,
// a new process, reads the limit the first one saved.
func TestSingleRunReplyLimitAcrossRuns(t *testing.T) {
	server := setupSingleRun(t)
	cfg.Files.Maildir = filepath.Join(t.TempDir(), "Maildir")
	dir := maildir.Dir(cfg.Files.Maildir)
	if err := dir.Create(); err != nil {
		t.Fatal(err)
	}
	request := []byte("From: carol@example.invalid\nSubject: remailer-help\n\nhelp\n")
	replies := func() int {
		server.mu.Lock()
		defer server.mu.Unlock()
		return len(slices.DeleteFunc(slices.Clone(server.rcpts), func(r string) bool { return r != "carol@example.invalid" }))
	}

	deliverMail(t, dir, request)
	if err := loopServer(); err != nil {
		t.Fatalf("first run: %s", err)
	}
	if n := replies(); n != 1 {
		t.Fatalf("first run: %d replies to carol, want 1", n)
	}

	// A new process: a fresh in-memory limiter, the databases released.
	fooReplies = newReplyLimiter()
	if ChunkDb != nil {
		ChunkDb.Close()
	}
	deliverMail(t, dir, request)
	if err := loopServer(); err != nil {
		t.Fatalf("second run: %s", err)
	}
	if n := replies(); n != 1 {
		t.Errorf("second run: %d replies to carol in all, want still 1", n)
	}
}
