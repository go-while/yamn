package keymgr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// touch gives file a modification time in the future, so KeyRefresh and
// StatRefresh see it as changed.
func touch(t *testing.T, file string) {
	t.Helper()
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(file, future, future); err != nil {
		t.Fatal(err)
	}
}

// TestReimportDropsRemovedKeys checks that a long-running process (the
// remailer daemon) drops keys that left pubring.mix when it reads it again,
// and that the stats are read again for the new keys.
func TestReimportDropsRemovedKeys(t *testing.T) {
	dir := t.TempDir()
	pr := filepath.Join(dir, "pubring.mix")
	st := filepath.Join(dir, "mlist2.txt")
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -1).Format(date_format)
	to := now.AddDate(0, 0, 10).Format(date_format)
	a := testKey{name: "alpha", addr: "alpha@example.org", seed: 1, from: from, to: to}
	b := testKey{name: "beta", addr: "beta@example.org", seed: 2, from: from, to: to}
	os.WriteFile(pr, testPubring(a, b), 0600)
	os.WriteFile(st, testStats(time.Hour-time.Since(mergeNow), "alpha 10 100", "beta 10 100"), 0600)
	p := NewPubring(pr, st)
	if err := p.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	if err := p.ImportStats(); err != nil {
		t.Fatal(err)
	}
	// beta's key is withdrawn (e.g. it lost the pinger quorum).
	os.WriteFile(pr, testPubring(a), 0600)
	touch(t, pr)
	if !p.KeyRefresh() {
		t.Fatal("expected refresh")
	}
	if err := p.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get("beta"); err == nil {
		t.Errorf("beta is still in the keyring after re-import (count=%d)", p.Count())
	}
	if _, err := p.Get("beta@example.org"); err == nil {
		t.Error("beta's address is still in the keyring after re-import")
	}
	// The new keys need their stats again.
	if !p.StatRefresh() {
		t.Fatal("stats not read again after the keyring changed")
	}
	p.ImportStats()
	if c := p.Candidates(0, 60, 90, true); len(c) != 1 || c[0] != "alpha@example.org" {
		t.Errorf("candidates after re-import: %v", c)
	}
}

// TestKeyRefreshMissingFile checks that a missing pubring.mix means there's
// nothing to refresh, instead of a panic in the daemon's hourly run.
func TestKeyRefreshMissingFile(t *testing.T) {
	p := NewPubring(filepath.Join(t.TempDir(), "missing.mix"), "x")
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("KeyRefresh panics on a missing pubring: %v", r)
		}
	}()
	if p.KeyRefresh() {
		t.Error("KeyRefresh true without a file")
	}
}

// TestExpiredKeyNotUsed checks that a key that expires after it was
// imported is no longer used, unless use_expired is set.
func TestExpiredKeyNotUsed(t *testing.T) {
	p := NewPubring("", "")
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	old := Remailer{name: "old", Address: "old@example.org", caps: "E",
		from: yesterday.AddDate(0, 0, -14), until: yesterday, uptime: 1000}
	cur := Remailer{name: "cur", Address: "cur@example.org", caps: "E",
		from: yesterday, until: yesterday.AddDate(0, 0, 14), uptime: 1000}
	p.Put(old)
	p.Put(cur)
	for _, ref := range []string{"old", "old@example.org"} {
		if _, err := p.Get(ref); err == nil {
			t.Errorf("Get(%s): expired key returned", ref)
		}
	}
	if _, err := p.Get("cur"); err != nil {
		t.Error(err)
	}
	if c := p.Candidates(0, 60, 0, true); len(c) != 1 || c[0] != "cur@example.org" {
		t.Errorf("candidates: %v", c)
	}
	// The key is good until the end of its last day.
	old.until = time.Now().UTC()
	p.Put(old)
	if _, err := p.Get("old"); err != nil {
		t.Errorf("key valid until today rejected: %s", err)
	}
	old.until = yesterday
	p.Put(old)
	p.UseExpired()
	if _, err := p.Get("old"); err != nil {
		t.Errorf("use_expired: %s", err)
	}
	if c := p.Candidates(0, 60, 0, true); len(c) != 2 {
		t.Errorf("use_expired candidates: %v", c)
	}
}
