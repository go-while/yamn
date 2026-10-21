package main

import (
	"bytes"
	"testing"
)

// TestDummyIgnoresChain checks that a dummy takes a random chain, not the
// one given with --chain for the real message.
func TestDummyIgnoresChain(t *testing.T) {
	_, exits := setupRandomExits(t)
	if err := Pubring.ImportStats(); err != nil {
		t.Fatal(err)
	}
	cfg.Files.Pooldir = t.TempDir()
	flag.Chain = exits[0].address
	// With --chain followed, every dummy would go to exita.  With a random
	// first hop, all 40 going there is very unlikely (at most 1/2 per dummy).
	for range 40 {
		dummy()
	}
	others := 0
	for _, f := range poolFiles(t, cfg.Files.Pooldir, "m") {
		if !bytes.Contains(f, []byte("\nTo: "+exits[0].address+"\n")) {
			others++
		}
	}
	if others == 0 {
		t.Errorf("All dummies went to %s, the --chain of the message", exits[0].address)
	}
}
