package main

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/crooks/yamn/keymgr"
)

// newTestSecring returns a secret keyring set up from cfg, as loopServer
// does.
func newTestSecring() *keymgr.Secring {
	s := keymgr.NewSecring(cfg.Files.Secring, cfg.Files.Pubkey)
	s.SetName("test")
	s.SetAddress("test@remailer.invalid")
	s.SetValidity(14, 28)
	s.SetVersion(version)
	return s
}

// restart reads the keyrings again, as a restarted remailer does, and runs
// the startup key checks.
func restart(t *testing.T) *keymgr.Secring {
	t.Helper()
	r := newTestSecring()
	if err := r.ImportSecring(); err != nil {
		t.Fatal(err)
	}
	startupKeys(r)
	return r
}

// TestStartupKeysAdvertisedNotInSecring checks that a key.txt advertising a
// key the secret keyring doesn't have is replaced at startup, even though
// another key is active.
func TestStartupKeysAdvertisedNotInSecring(t *testing.T) {
	setupTestConfig(t)
	s := newTestSecring()
	pubA, secA := eccGenerate()
	idA := s.Insert(pubA, secA)
	s.WriteSecret(idA)
	// A crash between writing the public and the secret key of B.
	pubB, secB := eccGenerate()
	idB := s.Insert(pubB, secB)
	s.WritePublic(pubB, idB)

	r := restart(t)
	adv, err := r.CheckAdvertised()
	if err != nil {
		t.Fatalf("after startup: %s", err)
	}
	if adv == idB || adv == idA {
		t.Errorf("advertising %s, expected a new key", adv)
	}
	// The new key's secret is on disk.
	if _, err := restart(t).GetSK(adv); err != nil {
		t.Errorf("new key not in secring.mix: %s", err)
	}
}

// TestStartupKeysPublicKeyMismatch checks that key.txt is replaced when its
// public key doesn't belong to the secret key of the same keyid.
func TestStartupKeysPublicKeyMismatch(t *testing.T) {
	setupTestConfig(t)
	s := newTestSecring()
	pubA, secA := eccGenerate()
	idA := s.Insert(pubA, secA)
	s.WriteSecret(idA)
	s.WritePublic(pubA, idA)
	pubB, _ := eccGenerate()
	b, _ := os.ReadFile(cfg.Files.Pubkey)
	b = []byte(strings.Replace(string(b), hex.EncodeToString(pubA), hex.EncodeToString(pubB), 1))
	os.WriteFile(cfg.Files.Pubkey, b, 0600)

	r := newTestSecring()
	r.ImportSecring()
	if _, err := r.CheckAdvertised(); err == nil || !strings.Contains(err.Error(), "doesn't match") {
		t.Fatalf("mismatch not detected: %v", err)
	}
	startupKeys(r)
	if adv, err := r.CheckAdvertised(); err != nil || adv == idA {
		t.Errorf("after startup: advertising %s, %v", adv, err)
	}
}

// TestStartupKeysKeepsGoodKey checks that a valid advertised key is kept.
func TestStartupKeysKeepsGoodKey(t *testing.T) {
	setupTestConfig(t)
	s := newTestSecring()
	pub, sec := eccGenerate()
	id := s.Insert(pub, sec)
	s.WriteSecret(id)
	s.WritePublic(pub, id)
	r := restart(t)
	if adv, err := r.CheckAdvertised(); err != nil || adv != id || r.Count() != 1 {
		t.Errorf("advertising %s (%v), %d keys; want %s and 1 key", adv, err, r.Count(), id)
	}
}

// TestGenerateKeypairSecretFirst checks that the secret key is stored
// before the public key is advertised: if writing key.txt fails, the
// secret is already in secring.mix.
func TestGenerateKeypairSecretFirst(t *testing.T) {
	setupTestConfig(t)
	// key.txt can't be written.
	if err := os.Mkdir(cfg.Files.Pubkey, 0700); err != nil {
		t.Fatal(err)
	}
	s := newTestSecring()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected WritePublic to fail")
			}
		}()
		generateKeypair(s)
	}()
	ids := s.ListKeyids()
	r := newTestSecring()
	r.ImportSecring()
	if len(ids) != 1 || r.Count() != 1 {
		t.Fatalf("secring.mix has %d keys, want the generated one", r.Count())
	}
	if _, err := r.GetSK(ids[0]); err != nil {
		t.Error(err)
	}
}
