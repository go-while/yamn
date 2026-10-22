package main

import (
	"os"
	"strings"
	"testing"

	"github.com/crooks/yamn/keymgr"
)

// pretendPinned changes the pin of r as if an earlier keyring had listed
// another key for it that is valid for years: the key in the current
// keyring is then an unexpected change.
func pretendPinned(t *testing.T, r *testRemailer) {
	t.Helper()
	pins, err := keymgr.LoadPins(cfg.Files.Keypins)
	if err != nil {
		t.Fatal(err)
	}
	p := pins.Pins[r.address]
	if p == nil {
		t.Fatalf("%s not pinned", r.name)
	}
	p.Keyid, p.Created, p.Expires = strings.Repeat("ab", 16), "2026-01-01", "2099-12-31"
	if err := pins.Save(); err != nil {
		t.Fatal(err)
	}
}

// TestKeyPinning checks key pinning from the config: the first import
// pins the remailers, an unexpected key change holds the remailer with a
// warning (once a day), and --pin-accept releases it.
func TestKeyPinning(t *testing.T) {
	middle, exit := setupRoundTrip(t)
	t.Cleanup(func() { dailyMu.Lock(); clear(dailyLogged); dailyMu.Unlock() })
	if cfg.Urls.KeyPinning != "hold" {
		t.Fatalf("default key_pinning %q, want hold", cfg.Urls.KeyPinning)
	}
	Pubring = newPubring()
	if err := importPubring(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.Files.Keypins); err != nil {
		t.Fatalf("no pin file: %v", err)
	}

	pretendPinned(t, middle)
	logged := captureLog(t)
	Pubring = newPubring()
	if err := importPubring(); err != nil {
		t.Fatal(err)
	}
	if _, err := Pubring.Get(middle.name); err == nil || !strings.Contains(err.Error(), "held by key pinning") {
		t.Errorf("Get(%s): %v", middle.name, err)
	}
	if _, err := Pubring.Get(exit.name); err != nil {
		t.Errorf("Get(%s): %v", exit.name, err)
	}
	if !Pubring.HasHeld() {
		t.Error("HasHeld is false")
	}
	if err := importPubring(); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(logged.String(), "Key pinning: "+middle.name); n != 1 {
		t.Errorf("warned %d times, want once:\n%s", n, logged)
	}

	flag.Pins = true
	if err := pinCommand(); err != nil {
		t.Errorf("--pins: %v", err)
	}
	flag.Pins, flag.PinAccept = false, middle.name
	if err := pinCommand(); err != nil {
		t.Fatalf("--pin-accept: %v", err)
	}
	if err := pinCommand(); err == nil {
		t.Error("--pin-accept twice succeeded")
	}
	Pubring = newPubring()
	if err := importPubring(); err != nil {
		t.Fatal(err)
	}
	if _, err := Pubring.Get(middle.name); err != nil {
		t.Errorf("after --pin-accept: %v", err)
	}
}

// TestKeyPinningReject checks --pin-reject: the change stays held.
func TestKeyPinningReject(t *testing.T) {
	middle, _ := setupRoundTrip(t)
	t.Cleanup(func() { dailyMu.Lock(); clear(dailyLogged); dailyMu.Unlock() })
	Pubring = newPubring()
	importPubring()
	pretendPinned(t, middle)
	Pubring = newPubring()
	importPubring()
	flag.PinReject = middle.address
	if err := pinCommand(); err != nil {
		t.Fatal(err)
	}
	Pubring = newPubring()
	importPubring()
	if _, err := Pubring.Get(middle.name); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Errorf("Get(%s): %v", middle.name, err)
	}
}

// TestKeyPinningOff checks that with key_pinning off no pins are kept.
func TestKeyPinningOff(t *testing.T) {
	setupRoundTrip(t)
	cfg.Urls.KeyPinning = "off"
	Pubring = newPubring()
	if err := importPubring(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.Files.Keypins); !os.IsNotExist(err) {
		t.Errorf("pin file with key_pinning off: %v", err)
	}
	if Pubring.Count() != 2 {
		t.Errorf("%d keys", Pubring.Count())
	}
}
