package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestReimportRemovedNextHop checks that a remailer whose key left
// pubring.mix is no longer accepted as a next hop once the running remailer
// reads the keyring again.
func TestReimportRemovedNextHop(t *testing.T) {
	middle, exit := setupRoundTrip(t)
	if err := checkNextHop(middle.address); err != nil {
		t.Fatal(err)
	}
	// Rewrite the pubring with only the exit's key.
	pubringFile := filepath.Join(flag.Dir, "pubring.mix")
	exitKey, err := os.ReadFile(filepath.Join(flag.Dir, "exit.key.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pubringFile, exitKey, 0600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	os.Chtimes(pubringFile, future, future)
	if !Pubring.KeyRefresh() {
		t.Fatal("KeyRefresh should see the new pubring")
	}
	if err := Pubring.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	if err := checkNextHop(middle.address); err == nil {
		t.Errorf("%s left pubring.mix but is still a valid next hop", middle.address)
	}
	if err := checkNextHop(exit.address); err != nil {
		t.Error(err)
	}
}
