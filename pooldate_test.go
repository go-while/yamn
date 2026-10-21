package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupPoolDateTest gives the test an empty pool and a fake SMTP relay.
func setupPoolDateTest(t *testing.T) *fakeSMTP {
	t.Helper()
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Pooldir = t.TempDir()
	cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
	return newFakeSMTP(t, nil, false, "")
}

// TestPooledDateContinuation checks that a plaintext starting with a space
// can't continue the internal Yamn-Pooled-Date header.  It used to make the
// date unparseable, and the file was kept on every pool run, never sent and
// never expired.
func TestPooledDateContinuation(t *testing.T) {
	setupPoolDateTest(t)
	// An exit pools the plaintext.
	if err := smtpMethod([]byte(" x\nTo: a@example.invalid\nSubject: hi\n\nbody\n"), testFinal(1, 1)); err != nil {
		t.Fatal(err)
	}
	names, err := readDir(cfg.Files.Pooldir, "m")
	if err != nil || len(names) != 1 {
		t.Fatalf("expected 1 pool file, got %v (%v)", names, err)
	}
	emailPoolFile(names[0])
	if _, err := os.Stat(filepath.Join(cfg.Files.Pooldir, names[0])); err == nil {
		t.Errorf("pool file %s is still in the pool after a pool run", names[0])
	}
}

// TestPooledDateFallback checks that a pool file whose date is missing or
// unparseable expires by its modification time, and that files written with
// the date header (also by older versions) are still sent.
func TestPooledDateFallback(t *testing.T) {
	old := time.Now().Add(-40 * 24 * time.Hour)
	tests := []struct {
		name     string
		header   string
		mtime    time.Time
		wantSent bool
	}{
		{"valid date", "Yamn-Pooled-Date: " + time.Now().Format(shortdate) + "\n", time.Now(), true},
		{"expired date", "Yamn-Pooled-Date: " + old.Format(shortdate) + "\n", time.Now(), false},
		{"bad date, new file", "Yamn-Pooled-Date: garbage\n", time.Now(), true},
		{"bad date, old file", "Yamn-Pooled-Date: garbage\n", old, false},
		{"no date, new file", "", time.Now(), true},
		{"no date, old file", "", old, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupPoolDateTest(t)
			file := filepath.Join(cfg.Files.Pooldir, "mtest")
			msg := tt.header + "To: a@example.invalid\nSubject: hi\n\nbody\n"
			if err := os.WriteFile(file, []byte(msg), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(file, tt.mtime, tt.mtime); err != nil {
				t.Fatal(err)
			}
			delFlag, err := mailPoolFile(file)
			_, _, delivered := server.result()
			if delivered != tt.wantSent {
				t.Fatalf("delivered = %v, want %v (err=%v)", delivered, tt.wantSent, err)
			}
			if !tt.wantSent && !delFlag {
				t.Errorf("an expired file should be marked for deletion")
			}
			server.mu.Lock()
			data := string(server.data)
			server.mu.Unlock()
			if strings.Contains(data, "Yamn-Pooled-Date") {
				t.Errorf("internal header reached the recipient: %q", data)
			}
		})
	}
}
