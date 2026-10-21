package keymgr

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestSecring(t *testing.T) *Secring {
	t.Helper()
	dir := t.TempDir()
	s := NewSecring(filepath.Join(dir, "secring.mix"), filepath.Join(dir, "key.txt"))
	s.SetValidity(14, 28)
	return s
}

// addSecret puts a key with the given dates into s and appends it to the
// secring file.
func addSecret(s *Secring, seed byte, from, until time.Time) string {
	k := secret{keyid: bytes.Repeat([]byte{seed}, 16), sk: bytes.Repeat([]byte{seed + 0x10}, 32),
		from: from, until: until}
	id := hex.EncodeToString(k.keyid)
	s.sec[id] = k
	s.WriteSecret(id)
	return id
}

// TestPurgeRemovesPurgedKey checks that a purged secret key doesn't stay on
// disk: not in secring.mix, nor in a secring.mix.tmp left by older versions.
func TestPurgeRemovesPurgedKey(t *testing.T) {
	s := newTestSecring(t)
	now := time.Now().UTC()
	oldID := addSecret(s, 1, now.AddDate(0, 0, -100), now.AddDate(0, 0, -60))
	curID := addSecret(s, 2, now.AddDate(0, 0, -1), now.AddDate(0, 0, 13))
	oldSK := hex.EncodeToString(s.sec[oldID].sk)
	// A secring.mix.tmp as older versions left it.
	ring, _ := os.ReadFile(s.secringFile)
	os.WriteFile(s.secringFile+".tmp", ring, 0600)

	active, _, _, purged := s.Purge()
	if active != 1 || purged != 1 {
		t.Fatalf("active=%d purged=%d, want 1 and 1", active, purged)
	}
	dir := filepath.Dir(s.secringFile)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(string(b), oldSK) {
			t.Errorf("the purged secret key is still on disk in %s", e.Name())
		}
		if e.Name() != "secring.mix" {
			t.Errorf("Purge left %s behind", e.Name())
		}
	}
	// The remaining key reads back.
	r := NewSecring(s.secringFile, s.pubkeyFile)
	r.ImportSecring()
	if _, err := r.GetSK(curID); err != nil || r.Count() != 1 {
		t.Errorf("after Purge: count=%d, current key: %v", r.Count(), err)
	}
	if _, err := s.Get(oldID); err == nil {
		t.Error("purged key is still in memory")
	}
}

// TestPurgeWritesUTCDates checks that Purge writes the same UTC dates as
// WriteSecret and WritePublic, whatever the local time zone.
func TestPurgeWritesUTCDates(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("UTC-5", -5*3600)
	t.Cleanup(func() { time.Local = oldLocal })
	s := newTestSecring(t)
	// 02:00 UTC on Nov 4 is 21:00 on Nov 3 in UTC-5.
	until := time.Date(2026, 11, 4, 2, 0, 0, 0, time.UTC).In(time.Local)
	addSecret(s, 3, until.AddDate(0, 0, -14), until)
	s.Purge()
	b, _ := os.ReadFile(s.secringFile)
	if !strings.Contains(string(b), "Created: 2026-10-21\nExpires: 2026-11-04\n") {
		t.Errorf("Purge didn't write UTC dates:\n%s", b)
	}
	// Insert stores UTC times too.
	id := s.Insert(bytes.Repeat([]byte{4}, 32), bytes.Repeat([]byte{5}, 32))
	if loc := s.sec[id].from.Location(); loc != time.UTC {
		t.Errorf("Insert stores times in %s", loc)
	}
}

// TestWriteFileSyncFailureKeepsOriginal checks that a failed rewrite leaves
// the original file as it was.
func TestWriteFileSyncFailureKeepsOriginal(t *testing.T) {
	file := filepath.Join(t.TempDir(), "secring.mix")
	os.WriteFile(file, []byte("original\n"), 0600)
	// A non-empty directory where the new file should go can't be
	// replaced.
	os.MkdirAll(filepath.Join(file+".new", "x"), 0700)
	if err := writeFileSync(file, []byte("new\n")); err == nil {
		t.Fatal("expected an error")
	}
	if b, _ := os.ReadFile(file); string(b) != "original\n" {
		t.Errorf("original changed: %q", b)
	}
	os.RemoveAll(file + ".new")
	if err := writeFileSync(file, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != "new\n" {
		t.Errorf("not replaced: %q", b)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0600 {
		t.Errorf("mode %v, want 0600", st.Mode().Perm())
	}
}

// TestImportSecringBadSecretHex checks that a secret key line with trailing
// non-hex is rejected, even though 32 bytes decode before it.
func TestImportSecringBadSecretHex(t *testing.T) {
	file := filepath.Join(t.TempDir(), "secring.mix")
	today := time.Now().UTC().Format(date_format)
	later := time.Now().UTC().AddDate(0, 0, 14).Format(date_format)
	sk := strings.Repeat("ab", 32)
	ring := "-----Begin Mixmaster Secret Key-----\nCreated: " + today + "\nExpires: " + later + "\n" +
		strings.Repeat("cd", 16) + "\n" + sk + "zz\n-----End Mixmaster Secret Key-----\n"
	os.WriteFile(file, []byte(ring), 0600)
	s := NewSecring(file, filepath.Join(t.TempDir(), "key.txt"))
	s.ImportSecring()
	if s.Count() != 0 {
		t.Errorf("secret key line %q (not valid hex) was accepted", sk+"zz")
	}
}
