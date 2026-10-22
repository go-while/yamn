package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crooks/yamn/config"
	"github.com/crooks/yamn/keymgr"
)

// setupSelfCheck returns this remailer's secret keyring with one key, its
// key.txt, and the key.txt of a key with the same name and address that
// someone else made.
func setupSelfCheck(t *testing.T) (s *keymgr.Secring, own, forged string) {
	t.Helper()
	setupTestConfig(t)
	cfg.Remailer.Name = "test"
	cfg.Remailer.Address = "test@remailer.invalid"
	s = newTestSecring()
	generateKeypair(s)
	ownKey, err := os.ReadFile(cfg.Files.Pubkey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	other := keymgr.NewSecring(filepath.Join(dir, "secring.mix"), filepath.Join(dir, "key.txt"))
	other.SetName("test")
	other.SetAddress("test@remailer.invalid")
	other.SetValidity(14, 28)
	other.SetVersion(version)
	generateKeypair(other)
	forgedKey, err := os.ReadFile(filepath.Join(dir, "key.txt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		publishedMu.Lock()
		clear(publishedLogged)
		publishedMu.Unlock()
	})
	return s, string(ownKey), string(forgedKey)
}

func writeKeyring(t *testing.T, file string, blocks ...string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(strings.Join(blocks, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestCheckPublishedKeys checks which keyrings get a warning: a key for
// this remailer that isn't in its secret keyring, its name with another
// address, its address under another name.  Its own key and other
// remailers' keys don't.
func TestCheckPublishedKeys(t *testing.T) {
	s, own, forged := setupSelfCheck(t)
	otherRemailer := strings.ReplaceAll(forged, "test@remailer.invalid", "else@other.invalid")
	otherRemailer = strings.Replace(otherRemailer, "test ", "else ", 1)
	for _, tt := range []struct {
		name   string
		blocks []string
		want   string // part of the problem; "" for none
	}{
		{"own key", []string{own}, ""},
		{"own key and another remailer", []string{otherRemailer, own}, ""},
		{"only another remailer", []string{otherRemailer}, ""},
		{"empty", nil, ""},
		{"forged key", []string{forged}, "isn't in its secret keyring"},
		{"forged key, upper-case name", []string{strings.Replace(forged, "test ", "TEST ", 1)}, ""}, // not a valid name: ignored by clients
		{"name with another address", []string{strings.ReplaceAll(own, "test@remailer.invalid", "test@evil.invalid")}, "with the address test@evil.invalid"},
		{"address under another name", []string{strings.Replace(forged, "test ", "evil ", 1)}, "under the name evil"},
	} {
		writeKeyring(t, cfg.Files.Pubring, tt.blocks...)
		problems := checkPublishedKeys(s, time.Now())
		switch {
		case tt.want == "" && len(problems) > 0:
			t.Errorf("%s: unexpected problems %q", tt.name, problems)
		case tt.want != "" && (len(problems) != 1 || !strings.Contains(problems[0], tt.want)):
			t.Errorf("%s: problems %q, want one with %q", tt.name, problems, tt.want)
		}
	}
}

// TestCheckPublishedKeysPingers checks that with urls.pingers, each
// pinger's keyring is checked and the warning names the pinger.
func TestCheckPublishedKeysPingers(t *testing.T) {
	s, own, forged := setupSelfCheck(t)
	cfg.Urls.Pingers = []config.Pinger{{Name: "good"}, {Name: "bad"}, {Name: "missing"}}
	writeKeyring(t, pingerFile(cfg.Files.Pubring, "good"), own)
	writeKeyring(t, pingerFile(cfg.Files.Pubring, "bad"), forged)
	writeKeyring(t, cfg.Files.Pubring, own) // the merged one isn't checked
	problems := checkPublishedKeys(s, time.Now())
	if len(problems) != 1 || !strings.Contains(problems[0], "pinger bad publishes key") {
		t.Errorf("problems %q, want one for pinger bad", problems)
	}
}

// TestCheckPublishedKeysOncePerDay checks that a problem is logged once a
// day, not at every hourly check.
func TestCheckPublishedKeysOncePerDay(t *testing.T) {
	s, _, forged := setupSelfCheck(t)
	logged := captureLog(t)
	writeKeyring(t, cfg.Files.Pubring, forged)
	now := time.Now()
	count := func() int { return strings.Count(logged.String(), "isn't in its secret keyring") }
	checkPublishedKeys(s, now)
	checkPublishedKeys(s, now.Add(time.Hour))
	if n := count(); n != 1 {
		t.Errorf("logged %d times within an hour, want 1", n)
	}
	checkPublishedKeys(s, now.Add(25*time.Hour))
	if n := count(); n != 2 {
		t.Errorf("logged %d times after a day, want 2", n)
	}
}
