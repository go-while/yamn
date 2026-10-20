package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crooks/yamn/keymgr"
)

func TestCheckNextHop(t *testing.T) {
	middle, exit := setupRoundTrip(t)
	tests := []struct {
		addr    string
		wantErr bool
	}{
		{exit.address, false},
		{middle.address, false},
		{"unknown@remailer.invalid", true},
		{"exit", true}, // a remailer name, not an address
		{"", true},
		{exit.address + "\nCc: someone@example.invalid", true},
		{exit.address + "\r", true},
		{"exit\t@remailer.invalid", true},
	}
	for _, tt := range tests {
		err := checkNextHop(tt.addr)
		if (err != nil) != tt.wantErr {
			t.Errorf("checkNextHop(%q) error = %v, wantErr %v", tt.addr, err, tt.wantErr)
		}
	}
}

func TestWriteMessageToPool(t *testing.T) {
	middle, exit := setupRoundTrip(t)
	pool := actAs(t, middle, false)
	payload := make([]byte, messageBytes)

	if err := writeMessageToPool(exit.address+"\nCc: someone@example.invalid", payload); err == nil {
		t.Fatal("writeMessageToPool should refuse a recipient with a line break")
	}
	if n := len(poolFiles(t, pool, "m")); n != 0 {
		t.Fatalf("Refused message should not be pooled, got %d files", n)
	}

	if err := writeMessageToPool(exit.address, payload); err != nil {
		t.Fatalf("writeMessageToPool failed: %s", err)
	}
	files := poolFiles(t, pool, "m")
	if len(files) != 1 {
		t.Fatalf("Expected 1 pooled message, got %d", len(files))
	}
	if !bytes.Contains(files[0], []byte("To: "+exit.address+"\n")) {
		t.Fatalf("Pooled message not addressed to %s:\n%s", exit.address, files[0])
	}
}

// TestMiddleRefusesUnknownNextHop checks that a middle remailer does not
// forward a packet to a next hop that isn't in its own public keyring.  The
// packet is an ordinary one from encodeMsg; the middle just decodes it with a
// keyring that lacks the exit remailer.
func TestMiddleRefusesUnknownNextHop(t *testing.T) {
	middle, exit := setupRoundTrip(t)
	packet := encodeTestMsg([]byte("Hello"), middle, exit)

	// The middle's keyring knows nobody, so the exit is an unknown next hop.
	Pubring = keymgr.NewPubring(filepath.Join(t.TempDir(), "pubring.mix"), filepath.Join(t.TempDir(), "mlist2.txt"))
	pool := actAs(t, middle, false)
	err := decodeMsg(packet, middle.secring)
	if err == nil || !strings.Contains(err.Error(), "not a known remailer") {
		t.Fatalf("Expected the next hop to be refused, got: %v", err)
	}
	if n := len(poolFiles(t, pool, "")); n != 0 {
		t.Fatalf("Packet for an unknown next hop should not be pooled, got %d files", n)
	}
}
