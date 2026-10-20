package main

import (
	"bytes"
	"testing"
)

// TestImportRoundTrip takes the client's --stdout output, imports it into
// a fresh pool, and passes the pooled mails to the exit, which delivers the
// message once.
func TestImportRoundTrip(t *testing.T) {
	_, exit := setupRoundTrip(t)
	out := clientStdout(t, exit)

	importPool := t.TempDir()
	cfg.Files.Pooldir = importPool
	imported, errs := importMails(bytes.NewReader(out))
	if imported != 2 || len(errs) != 0 {
		t.Fatalf("importMails: imported %d, errors %v; want 2 and none", imported, errs)
	}
	pooled := poolFiles(t, importPool, "m")
	if len(pooled) != 2 {
		t.Fatalf("Expected 2 pooled mails, got %d", len(pooled))
	}
	// Each pooled mail carries the same packet as the corresponding
	// --stdout entry.
	for i, m := range splitMbox(out) {
		want, err := stripArmor(bytes.NewReader(m))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range pooled {
			if got, err := stripArmor(bytes.NewReader(p)); err == nil && bytes.Equal(got, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("Packet %d from --stdout not found in the pool", i+1)
		}
	}

	exitPool := deliverPool(t, importPool, exit, true)
	delivered := poolFiles(t, exitPool, "m")
	if len(delivered) != 1 || !bytes.Contains(delivered[0], []byte("Hello via stdout")) {
		t.Fatalf("Exit should deliver the message once, got %d messages", len(delivered))
	}
}

func TestImportRejectsInvalid(t *testing.T) {
	_, exit := setupRoundTrip(t)
	out := clientStdout(t, exit)
	good := splitMbox(out)[0]

	var mbox bytes.Buffer
	writeMbox(&mbox, []byte("To: someone@example.invalid\n\nJust a normal mail.\n"))
	writeMbox(&mbox, good)
	writeMbox(&mbox, []byte("Subject: no recipient\n\nhello\n"))

	cfg.Files.Pooldir = t.TempDir()
	imported, errs := importMails(&mbox)
	if imported != 1 || len(errs) != 2 {
		t.Fatalf("importMails: imported %d, errors %v; want 1 imported and 2 errors", imported, errs)
	}
	if n := len(poolFiles(t, cfg.Files.Pooldir, "m")); n != 1 {
		t.Fatalf("Only the valid mail should be pooled, got %d", n)
	}
}

func TestImportSingleMail(t *testing.T) {
	_, exit := setupRoundTrip(t)
	single := splitMbox(clientStdout(t, exit))[0]
	cfg.Files.Pooldir = t.TempDir()
	if imported, errs := importMails(bytes.NewReader(single)); imported != 1 || len(errs) != 0 {
		t.Fatalf("A single mail (not an mbox) should be imported, got %d, %v", imported, errs)
	}
}
