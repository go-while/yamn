package main

import (
	"strings"
	"testing"
)

// TestChainDistanceNamedHops checks that stats.distance also keeps random
// hops away from the hops named in the chain, on both sides, whether they
// are given by name or by address.
func TestChainDistanceNamedHops(t *testing.T) {
	_, exits := setupRandomExits(t)
	if err := Pubring.ImportStats(); err != nil {
		t.Fatal(err)
	}
	cfg.Stats.Distance = 2
	exita := exits[0]
	// With distance 2 and three remailers, every hop of a 3-hop chain must
	// be a different remailer.
	for _, tt := range []struct {
		in    string
		named int // position of exita
	}{
		{exita.name + ",*,*", 0},
		{exita.address + ",*,*", 0},
		{"*," + exita.name + ",*", 1},
		{"*,*," + exita.name, 2},
	} {
		for range 50 {
			got, err := makeChain(strings.Split(tt.in, ","))
			if err != nil {
				t.Fatalf("%s: %v", tt.in, err)
			}
			if len(got) != 3 || got[tt.named] != exita.address ||
				got[0] == got[1] || got[1] == got[2] || got[0] == got[2] {
				t.Fatalf("%s: got %q, want 3 different hops with %s at %d",
					tt.in, got, exita.address, tt.named)
			}
		}
	}
}

// TestChainNegativeDistance checks that a negative distance doesn't panic
// and means no distance at all.
func TestChainNegativeDistance(t *testing.T) {
	setupRandomExits(t)
	if err := Pubring.ImportStats(); err != nil {
		t.Fatal(err)
	}
	cfg.Stats.Distance = -1
	got, err := makeChain([]string{"*", "*"})
	if err != nil || len(got) != 2 {
		t.Fatalf("makeChain with distance -1: %q, %v", got, err)
	}
}

// TestChainNamedHopsUnchanged checks that makeChain doesn't change the
// caller's chain when it resolves named hops.
func TestChainNamedHopsUnchanged(t *testing.T) {
	_, exits := setupRandomExits(t)
	in := []string{"*", exits[0].name}
	if _, err := makeChain(in); err != nil {
		t.Fatal(err)
	}
	if in[0] != "*" || in[1] != exits[0].name {
		t.Errorf("makeChain changed its input: %q", in)
	}
}
