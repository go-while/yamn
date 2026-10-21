//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// holdPoolLock takes the pool lock as another process would and returns a
// function that releases it.
func holdPoolLock(t *testing.T) func() {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(cfg.Files.Pooldir, ".lock"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return func() { f.Close() }
}

// setupPoolLockTest gives the test a pool with one message and a fake SMTP
// relay.
func setupPoolLockTest(t *testing.T) (server *fakeSMTP, file string) {
	t.Helper()
	setupTestConfig(t)
	cfg.Mail.MXRelay = false
	cfg.Mail.SMTPRelay = "localhost"
	cfg.Files.Pooldir = t.TempDir()
	cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
	server = newFakeSMTP(t, nil, false, "")
	return server, writePoolMessage(t, "To: a@example.invalid\n")
}

// TestPoolFlushLocked checks that "yamn -S" doesn't send from the pool while
// another process (a --daemon remailer) holds the pool lock.
func TestPoolFlushLocked(t *testing.T) {
	server, file := setupPoolLockTest(t)
	release := holdPoolLock(t)
	poolOutboundSend()
	if _, _, delivered := server.result(); delivered {
		t.Fatal("the pool was sent while another process held the lock")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the pool file should be kept: %s", err)
	}
	release()
	poolOutboundSend()
	if _, _, delivered := server.result(); !delivered {
		t.Fatal("the pool should be sent once the lock is free")
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("the sent pool file should be deleted")
	}
}

// TestPoolBatchLocked checks the same for the daemon's pool runs.
func TestPoolBatchLocked(t *testing.T) {
	server, _ := setupPoolLockTest(t)
	cfg.Pool.Size, cfg.Pool.MinSend, cfg.Pool.Rate = 0, 1, 100
	release := holdPoolLock(t)
	sendPoolBatch()
	if _, _, delivered := server.result(); delivered {
		t.Fatal("a pool run sent mail while another process held the lock")
	}
	release()
	sendPoolBatch()
	if _, _, delivered := server.result(); !delivered {
		t.Fatal("the pool run should send once the lock is free")
	}
}
