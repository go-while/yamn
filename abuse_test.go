package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDestBlock(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dest.blk")
	content := "# Opted out\nalice@example.invalid\n  BOB@Example.invalid  \n\n@blocked.invalid\n"
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	list, err := readDestBlock(file)
	if err != nil {
		t.Fatal(err)
	}
	for addy, want := range map[string]bool{
		"alice@example.invalid":       true,
		"Alice@EXAMPLE.invalid":       true,
		"bob@example.invalid":         true,
		"anyone@blocked.invalid":      true,
		"carol@example.invalid":       false,
		"someone@sub.blocked.invalid": true, // subdomains too
		"someone@notblocked.invalid":  false,
		"# opted out":                 false,
	} {
		if got := destBlocked(list, addy); got != want {
			t.Errorf("destBlocked(%q) = %v, want %v", addy, got, want)
		}
	}
	missing, err := readDestBlock(filepath.Join(t.TempDir(), "missing.blk"))
	if err != nil || len(missing) != 0 {
		t.Errorf("A missing block list should be empty, got %v, %v", missing, err)
	}
}

// writePoolMessage writes a pooled exit message and returns its filename.
func writePoolMessage(t *testing.T, headers string) string {
	t.Helper()
	file := filepath.Join(cfg.Files.Pooldir, "m"+strings.ReplaceAll(t.Name(), "/", "_")+time.Now().Format("150405.000000000"))
	msg := "Yamn-Pooled-Date: " + time.Now().Format(shortdate) + "\n" + headers + "Subject: hi\n\nbody\n"
	if err := os.WriteFile(file, []byte(msg), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestMailPoolFileDestBlock(t *testing.T) {
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Pooldir = t.TempDir()
	cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
	if err := os.WriteFile(cfg.Files.DestBlock, []byte("alice@example.invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("blocked recipient removed", func(t *testing.T) {
		server := newFakeSMTP(t, nil, false, "")
		file := writePoolMessage(t, "To: alice@example.invalid\nCc: carol@example.invalid\n")
		if _, err := mailPoolFile(file); err != nil {
			t.Fatalf("mailPoolFile failed: %s", err)
		}
		server.mu.Lock()
		rcpts := slices.Clone(server.rcpts)
		server.mu.Unlock()
		if !slices.Equal(rcpts, []string{"carol@example.invalid"}) {
			t.Errorf("Recipients: got %q, want only carol", rcpts)
		}
	})
	t.Run("all recipients blocked", func(t *testing.T) {
		server := newFakeSMTP(t, nil, false, "")
		file := writePoolMessage(t, "To: alice@example.invalid\n")
		delFlag, err := mailPoolFile(file)
		if err == nil || !delFlag {
			t.Fatalf("Expected an error and deletion, got err=%v delFlag=%v", err, delFlag)
		}
		if _, _, delivered := server.result(); delivered {
			t.Error("Message to a blocked recipient was delivered")
		}
	})
	t.Run("unreadable block list keeps the message", func(t *testing.T) {
		server := newFakeSMTP(t, nil, false, "")
		cfg.Files.DestBlock = t.TempDir() // a directory: reading it fails
		file := writePoolMessage(t, "To: carol@example.invalid\n")
		delFlag, err := mailPoolFile(file)
		if err == nil || delFlag {
			t.Fatalf("Expected an error without deletion, got err=%v delFlag=%v", err, delFlag)
		}
		if _, _, delivered := server.result(); delivered {
			t.Error("Message delivered although the block list couldn't be read")
		}
	})
}

func TestExitMaxSize(t *testing.T) {
	t.Run("single chunk", func(t *testing.T) {
		pool := setupChunkDb(t)
		cfg.Remailer.MaxSize = 1 // kB
		if err := smtpMethod(make([]byte, 2000), testFinal(1, 1)); err == nil {
			t.Error("A 2000 byte message should exceed max_size of 1 kB")
		}
		if err := smtpMethod(make([]byte, 500), testFinal(1, 1)); err != nil {
			t.Errorf("A 500 byte message should be accepted: %s", err)
		}
		cfg.Remailer.MaxSize = 0
		if err := smtpMethod(make([]byte, 2000), testFinal(1, 1)); err != nil {
			t.Errorf("max_size 0 should mean no limit: %s", err)
		}
		if n := len(poolFiles(t, pool, "m")); n != 2 {
			t.Errorf("Expected 2 pooled messages, got %d", n)
		}
	})
	t.Run("too many chunks rejected at the first chunk", func(t *testing.T) {
		pool := setupChunkDb(t)
		cfg.Remailer.MaxSize = 20 // kB; 3 chunks are at least 2*17910 bytes
		if err := smtpMethod(make([]byte, maxFragLength), testFinal(1, 3)); err == nil {
			t.Error("A 3-chunk message should exceed max_size of 20 kB")
		}
		if n := len(poolFiles(t, pool, "")); n != 0 {
			t.Errorf("Rejected chunk should not be pooled, got %d files", n)
		}
	})
	t.Run("exact size checked on assembly", func(t *testing.T) {
		pool := setupChunkDb(t)
		cfg.Remailer.MaxSize = 18 // kB = 18432 bytes; total below is 18910
		first := testFinal(1, 2)
		second := testFinal(2, 2)
		second.messageID = first.messageID
		if err := smtpMethod(bytes.Repeat([]byte("a"), maxFragLength), first); err != nil {
			t.Fatalf("First chunk should be accepted: %s", err)
		}
		if err := smtpMethod(bytes.Repeat([]byte("b"), 1000), second); err == nil {
			t.Error("Assembled message of 18910 bytes should exceed max_size of 18 kB")
		}
		if n := len(poolFiles(t, pool, "")); n != 0 {
			t.Errorf("Oversized message's chunks should be deleted, got %d files", n)
		}
		if items := ChunkDb.Get(first.messageID, 2); items[0] != "" || items[1] != "" {
			t.Errorf("Chunk record should be deleted, got %q", items)
		}
	})
}
