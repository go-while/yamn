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
	l1 := newReplyLimiter()
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
	if err := l2.load(file); err != nil {
		t.Fatalf("load: %v", err)
	}
	if l2.allowed("alice@example.invalid") {
		t.Error("the next process allows a second reply on the same day")
	}
	if !l2.allowed("bob@example.invalid") {
		t.Error("another address is blocked")
	}
	tomorrow := time.Now().Add(24 * time.Hour)
	l2.now = func() time.Time { return tomorrow }
	if !l2.allowed("alice@example.invalid") {
		t.Error("still blocked on the next day")
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
