package keymgr

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestImportStatsMalformedLatency checks that a stats line whose latency
// field has no ":" is skipped instead of crashing the import.
func TestImportStatsMalformedLatency(t *testing.T) {
	pubring, mlist2 := writeTestFiles(t)
	// Break the latency field of test03 ("   :03  " becomes "   03  ").
	stats, err := os.ReadFile(mlist2)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(stats), "   :03  ", "   03   ", 1)
	if broken == string(stats) {
		t.Fatal("Test fixture did not contain the expected latency field")
	}
	if err := os.WriteFile(mlist2, []byte(broken), 0644); err != nil {
		t.Fatal(err)
	}

	p := NewPubring(pubring, mlist2)
	if err := p.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	if err := p.ImportStats(); err != nil {
		t.Fatalf("ImportStats failed: %s", err)
	}
	// test03 has no usable stats, so it no longer qualifies as an exit.
	// The other three exits (test00-test02) still do.
	if n := len(p.Candidates(1, 8, 70.0, true)); n != 2 {
		t.Fatalf("Expected 2 exit candidates, got %d", n)
	}
}

// TestImportSecringMalformedLines checks that short or missing date lines in
// a secret keyring are skipped instead of crashing the import, and that a
// valid key after them is still read.
func TestImportSecringMalformedLines(t *testing.T) {
	keyid := bytes.Repeat([]byte{0xab}, 16)
	sk := bytes.Repeat([]byte{0xcd}, 32)
	good := "-----Begin Mixmaster Secret Key-----\n" +
		"Created: 2020-01-01\n" +
		"Expires: 2100-01-01\n" +
		hex.EncodeToString(keyid) + "\n" +
		hex.EncodeToString(sk) + "\n" +
		"-----End Mixmaster Secret Key-----\n"
	content := "-----Begin Mixmaster Secret Key-----\n\n" + // empty Created line
		"-----Begin Mixmaster Secret Key-----\nCreated: 2020-01-01\nshort\n" + // short Expires line
		good
	secring := filepath.Join(t.TempDir(), "secring.mix")
	if err := os.WriteFile(secring, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	s := NewSecring(secring, filepath.Join(t.TempDir(), "key.txt"))
	if err := s.ImportSecring(); err != nil {
		t.Fatalf("ImportSecring failed: %s", err)
	}
	got, err := s.GetSK(hex.EncodeToString(keyid))
	if err != nil {
		t.Fatalf("Valid key after malformed blocks not imported: %s", err)
	}
	if !bytes.Equal(got, sk) {
		t.Fatal("Imported secret key does not match")
	}
}
