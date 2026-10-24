//go:build unix

package main

import (
	"os"
	"testing"
)

// TestRootIDs checks that a real or effective user ID of 0 counts as root.
func TestRootIDs(t *testing.T) {
	for _, c := range []struct {
		uid, euid int
		root      bool
	}{
		{1000, 1000, false},
		{0, 0, true},
		{0, 1000, true}, // can switch back to root
		{1000, 0, true}, // setuid root
	} {
		if got := rootIDs(c.uid, c.euid); got != c.root {
			t.Errorf("rootIDs(%d, %d) = %v, want %v", c.uid, c.euid, got, c.root)
		}
	}
	who, ok := privileged()
	if want := os.Getuid() == 0 || os.Geteuid() == 0; ok != want || who != "root" {
		t.Errorf("privileged() = %q, %v; running as uid %d euid %d", who, ok, os.Getuid(), os.Geteuid())
	}
}
