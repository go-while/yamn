package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestReplyLimitPersists checks that a limiter with a file keeps the day's
// replies for the next process, until the next UTC day.
func TestReplyLimitPersists(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".replylimit")
	noon := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	l1 := newReplyLimiter()
	l1.now = func() time.Time { return noon }
	if err := l1.load(file); err != nil {
		t.Fatalf("load without a file: %v", err)
	}
	if err := l1.record("Alice@example.invalid"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("file %v, err %v; want mode 0600", fi, err)
	}
	l2 := newReplyLimiter()
	l2.now = func() time.Time { return noon }
	if err := l2.load(file); err != nil {
		t.Fatalf("load: %v", err)
	}
	if l2.allowed("alice@example.invalid") {
		t.Error("the next process allows a second reply on the same day")
	}
	if !l2.allowed("bob@example.invalid") {
		t.Error("another address is blocked")
	}
	tomorrow := noon.Add(24 * time.Hour)
	l2.now = func() time.Time { return tomorrow }
	if !l2.allowed("alice@example.invalid") {
		t.Error("still blocked on the next day")
	}
	// A process started the next day doesn't keep the old day's hashes.
	l3 := newReplyLimiter()
	l3.now = func() time.Time { return tomorrow }
	if err := l3.load(file); err != nil || !l3.allowed("alice@example.invalid") || len(l3.sent) != 0 {
		t.Errorf("next-day load: err %v, kept %d hashes", err, len(l3.sent))
	}
}

// TestReplyLimitOversizedFile checks that a file over the cap isn't read
// into memory as hashes: it is reported and replaced.
func TestReplyLimitOversizedFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".replylimit")
	big := append([]byte("YRL1"), make([]byte, maxReplyLimitFile)...)
	if err := os.WriteFile(file, big, 0600); err != nil {
		t.Fatal(err)
	}
	l := newReplyLimiter()
	if err := l.load(file); err == nil {
		t.Error("an oversized file was accepted")
	}
	if len(l.sent) != 0 {
		t.Errorf("%d hashes loaded", len(l.sent))
	}
}

// TestReplyLimitDamagedFile checks that a damaged file is reported and
// replaced, not trusted.
func TestReplyLimitDamagedFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".replylimit")
	if err := os.WriteFile(file, []byte("YRL1 short"), 0600); err != nil {
		t.Fatal(err)
	}
	l := newReplyLimiter()
	if err := l.load(file); err == nil {
		t.Error("a damaged file was accepted")
	}
	if err := l.record("carol@example.invalid"); err != nil {
		t.Fatalf("record: %v", err)
	}
	l2 := newReplyLimiter()
	if err := l2.load(file); err != nil || l2.allowed("carol@example.invalid") {
		t.Errorf("rewritten file: err %v, allowed %v", err, l2.allowed("carol@example.invalid"))
	}
}

// TestReplyLimitFull checks that a day with the most hashes the file can
// hold allows no more replies, so a full day's file stays loadable.
func TestReplyLimitFull(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".replylimit")
	l := newReplyLimiter()
	if err := l.load(file); err != nil {
		t.Fatal(err)
	}
	l.rollover()
	for i := 0; len(l.sent) < maxReplyHashes-1; i++ {
		l.sent[uint64(i)] = struct{}{}
	}
	if !l.allowed("last@example.invalid") {
		t.Fatal("refused before the set is full")
	}
	if err := l.record("last@example.invalid"); err != nil {
		t.Fatal(err)
	}
	if l.allowed("another@example.invalid") {
		t.Error("allowed a reply with the day's set full")
	}
	if fi, err := os.Stat(file); err != nil || fi.Size() > maxReplyLimitFile {
		t.Fatalf("file %v, err %v; want at most %d bytes", fi, err, maxReplyLimitFile)
	}
	l2 := newReplyLimiter()
	if err := l2.load(file); err != nil || len(l2.sent) != maxReplyHashes {
		t.Errorf("reload: err %v, %d hashes, want %d", err, len(l2.sent), maxReplyHashes)
	}
}
