package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luksen/maildir"
)

// deliverMail drops a message into the remailer's Maildir, as an MTA would.
func deliverMail(t *testing.T, dir maildir.Dir, msg []byte) {
	t.Helper()
	d, err := dir.NewDelivery()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Write(msg); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

// dirFiles returns the contents of the regular files in dir.
func dirFiles(t *testing.T, dir string) (files []string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, string(b))
	}
	return
}

// TestProcessMailRejectsNonYamn checks that mail which isn't a yamn message
// is moved to the rejected folder, while a yamn packet arriving alongside it
// is still decoded and delivered.
func TestProcessMailRejectsNonYamn(t *testing.T) {
	_, exit := setupRoundTrip(t)
	pool := actAs(t, exit, true)
	cfg.Files.Maildir = filepath.Join(t.TempDir(), "Maildir")
	dir := maildir.Dir(cfg.Files.Maildir)
	if err := dir.Create(); err != nil {
		t.Fatal(err)
	}

	deliverMail(t, dir, []byte("From: spammer@example.invalid\nSubject: Offer\n\nBuy now!\n"))
	packet := new(bytes.Buffer)
	packet.WriteString("To: " + exit.address + "\nSubject: yamn\n\n")
	armor(packet, encodeTestMsg([]byte("Hello from yamn"), exit))
	deliverMail(t, dir, packet.Bytes())

	if err := processMail(exit.secring); err != nil {
		t.Fatalf("processMail failed: %s", err)
	}
	for _, sub := range []string{"new", "cur"} {
		if n := len(dirFiles(t, filepath.Join(cfg.Files.Maildir, sub))); n != 0 {
			t.Errorf("Maildir/%s should be empty, has %d messages", sub, n)
		}
	}
	rejected := dirFiles(t, filepath.Join(cfg.Files.Maildir, ".rejected", "cur"))
	if len(rejected) != 1 || !strings.Contains(rejected[0], "Buy now!") {
		t.Fatalf("Expected the spam message in .rejected, got %d messages", len(rejected))
	}
	delivered := poolFiles(t, pool, "m")
	if len(delivered) != 1 || !bytes.HasSuffix(delivered[0], []byte("Hello from yamn")) {
		t.Fatalf("The yamn packet should be delivered, got %d pooled messages", len(delivered))
	}
}

func TestExpireRejected(t *testing.T) {
	setupTestConfig(t)
	cfg.Files.Maildir = filepath.Join(t.TempDir(), "Maildir")
	cfg.Pool.MaxAge = 28
	// No rejected folder yet: nothing to do, and no error.
	expireRejected()

	// The remailer creates its Maildir at startup; the rejected folder
	// lives inside it.
	if err := maildir.Dir(cfg.Files.Maildir).Create(); err != nil {
		t.Fatal(err)
	}
	rejected := rejectedMaildir()
	if err := rejected.Create(); err != nil {
		t.Fatal(err)
	}
	cur := filepath.Join(string(rejected), "cur")
	for name, age := range map[string]time.Duration{
		"old:2,":    30 * 24 * time.Hour,
		"recent:2,": 2 * 24 * time.Hour,
	} {
		file := filepath.Join(cur, name)
		if err := os.WriteFile(file, []byte("Subject: x\n\nx\n"), 0600); err != nil {
			t.Fatal(err)
		}
		mtime := time.Now().Add(-age)
		if err := os.Chtimes(file, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	expireRejected()
	entries, err := os.ReadDir(cur)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "recent:2," {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("Expected only the recent message to remain, got %q", names)
	}
}
