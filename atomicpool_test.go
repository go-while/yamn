package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/luksen/maildir"
)

// TestAssembleMissingChunk checks that a multi-chunk message with a missing
// chunk file isn't assembled without it: nothing is pooled for delivery and
// the other chunks are kept.
func TestAssembleMissingChunk(t *testing.T) {
	pool := setupChunkDb(t)
	first := testFinal(1, 2)
	second := testFinal(2, 2)
	second.messageID = first.messageID
	if err := smtpMethod([]byte("part one"), first); err != nil {
		t.Fatal(err)
	}
	names, err := readDir(pool, "p")
	if err != nil || len(names) != 1 {
		t.Fatalf("expected 1 pooled chunk, got %v (%v)", names, err)
	}
	if err := os.Remove(filepath.Join(pool, names[0])); err != nil {
		t.Fatal(err)
	}
	if err := smtpMethod([]byte("part two"), second); err == nil {
		t.Error("assembling a message with a missing chunk should fail")
	}
	if n := len(poolFiles(t, pool, "m")); n != 0 {
		t.Errorf("a message with a missing chunk was pooled for delivery (%d files)", n)
	}
	if n := len(poolFiles(t, pool, "p")); n != 1 {
		t.Errorf("the chunk that arrived should be kept, %d chunk files left", n)
	}
}

// TestPoolReaderSeesNoPartialFile assembles a large multi-chunk message while
// another goroutine reads the outbound pool, as the daemon's pool sender
// does.  The reader must never see a file before it's complete.
func TestPoolReaderSeesNoPartialFile(t *testing.T) {
	pool := setupChunkDb(t)
	const n = 255
	plain := bytes.Repeat([]byte("A"), maxFragLength)
	for attempt := 0; attempt < 3; attempt++ {
		base := testFinal(1, n)
		for c := 1; c < n; c++ {
			f := testFinal(c, n)
			f.messageID = base.messageID
			if err := smtpMethod(plain, f); err != nil {
				t.Fatal(err)
			}
		}
		var done atomic.Bool
		var partial atomic.Int64
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !done.Load() {
				names, _ := readDir(pool, "m")
				for _, name := range names {
					b, err := os.ReadFile(filepath.Join(pool, name))
					if err == nil && len(b) < n*maxFragLength {
						partial.Store(int64(len(b)))
					}
				}
			}
		}()
		last := testFinal(n, n)
		last.messageID = base.messageID
		err := smtpMethod(plain, last)
		done.Store(true)
		wg.Wait()
		if err != nil {
			t.Fatal(err)
		}
		if got := partial.Load(); got > 0 {
			t.Fatalf("a pool reader saw a file with %d of %d bytes while it was being written", got, n*maxFragLength)
		}
		for _, name := range poolFilesNames(t, pool, "m") {
			os.Remove(filepath.Join(pool, name))
		}
	}
}

func poolFilesNames(t *testing.T, dir, prefix string) []string {
	t.Helper()
	names, err := readDir(dir, prefix)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// TestProcessMailKeepsMailOnPoolError checks that a message which can't be
// written to the pool stays in the Maildir, and is delivered on the next
// run once the pool works again.
func TestProcessMailKeepsMailOnPoolError(t *testing.T) {
	_, exit := setupRoundTrip(t)
	pool := actAs(t, exit, true)
	cfg.Files.Maildir = filepath.Join(t.TempDir(), "Maildir")
	dir := maildir.Dir(cfg.Files.Maildir)
	if err := dir.Create(); err != nil {
		t.Fatal(err)
	}
	packet := new(bytes.Buffer)
	packet.WriteString("To: " + exit.address + "\nSubject: yamn\n\n")
	armor(packet, encodeTestMsg([]byte("Hello from yamn"), exit))
	deliverMail(t, dir, packet.Bytes())

	// The pool can't be written.
	cfg.Files.Pooldir = filepath.Join(pool, "missing")
	processMail(exit.secring)
	if n, err := dir.UnseenCount(); err != nil || n != 1 {
		t.Fatalf("the mail should be kept for the next run, %d unseen (%v)", n, err)
	}

	cfg.Files.Pooldir = pool
	processMail(exit.secring)
	if n, _ := dir.UnseenCount(); n != 0 {
		t.Errorf("the mail should be processed on the next run, %d unseen", n)
	}
	delivered := poolFiles(t, pool, "m")
	if len(delivered) != 1 || !bytes.HasSuffix(delivered[0], []byte("Hello from yamn")) {
		t.Fatalf("the message should be pooled on the next run, got %d pool files", len(delivered))
	}
}

// TestProcessMailReadsCur checks that mail already moved to cur, e.g. by an
// earlier run whose move from new failed part way, is still processed.
func TestProcessMailReadsCur(t *testing.T) {
	_, exit := setupRoundTrip(t)
	pool := actAs(t, exit, true)
	cfg.Files.Maildir = filepath.Join(t.TempDir(), "Maildir")
	dir := maildir.Dir(cfg.Files.Maildir)
	if err := dir.Create(); err != nil {
		t.Fatal(err)
	}
	packet := new(bytes.Buffer)
	packet.WriteString("To: " + exit.address + "\nSubject: yamn\n\n")
	armor(packet, encodeTestMsg([]byte("Hello from yamn"), exit))
	deliverMail(t, dir, packet.Bytes())
	// Move it to cur, as an interrupted run would have.
	if keys, err := dir.Unseen(); err != nil || len(keys) != 1 {
		t.Fatalf("Unseen: %v, %v", keys, err)
	}

	if err := processMail(exit.secring); err != nil {
		t.Fatalf("processMail failed: %s", err)
	}
	if keys, _ := dir.Keys(); len(keys) != 0 {
		t.Errorf("Maildir/cur should be empty, has %d messages", len(keys))
	}
	delivered := poolFiles(t, pool, "m")
	if len(delivered) != 1 || !bytes.HasSuffix(delivered[0], []byte("Hello from yamn")) {
		t.Fatalf("the message in cur should be pooled, got %d pool files", len(delivered))
	}
}
