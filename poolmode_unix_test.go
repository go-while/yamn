//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestPoolFileModes checks that assembled multi-chunk messages and outfile
// deliveries are only readable by the owner, like other pool files, whatever
// the umask.
func TestPoolFileModes(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	pool := setupChunkDb(t)
	first := testFinal(1, 2)
	second := testFinal(2, 2)
	second.messageID = first.messageID
	if err := smtpMethod([]byte("part one"), first); err != nil {
		t.Fatal(err)
	}
	if err := smtpMethod([]byte("part two"), second); err != nil {
		t.Fatal(err)
	}
	cfg.Mail.Outfile = true
	if err := mailBytes([]byte("Subject: x\n\nbody\n"), []string{"a@example.invalid"}); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"m", "outfile-"} {
		names, err := readDir(pool, prefix)
		if err != nil || len(names) != 1 {
			t.Fatalf("expected 1 %q file, got %v (%v)", prefix, names, err)
		}
		info, err := os.Stat(filepath.Join(pool, names[0]))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s has mode %v, want 0600", names[0], perm)
		}
	}
}
