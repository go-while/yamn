package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNumCopies(t *testing.T) {
	setupTestConfig(t)
	for _, tt := range []struct{ flagCopies, configCopies, want int }{
		{0, 2, 2}, // no flag: config applies
		{0, 3, 3},
		{3, 2, 3}, // flag wins
		{1, 2, 1},
		{0, 0, 1}, // never zero copies
		{9, 2, maxCopies},
		{0, 7, maxCopies},
		{-1, 2, 2},
	} {
		flag.Copies, cfg.Stats.Numcopies = tt.flagCopies, tt.configCopies
		if got := numCopies(); got != tt.want {
			t.Errorf("--copies=%d, num_copies=%d: got %d, want %d", tt.flagCopies, tt.configCopies, got, tt.want)
		}
	}
}

// TestDefaultNumCopies checks the defaults still send 2 copies, as before
// num_copies took effect.
func TestDefaultNumCopies(t *testing.T) {
	setupTestConfig(t)
	if got := numCopies(); got != 2 {
		t.Errorf("Default copies: got %d, want 2", got)
	}
}

// TestMixprepUsesConfigCopies runs the real client without --copies and
// checks it sends the number of copies set in the config.
func TestMixprepUsesConfigCopies(t *testing.T) {
	_, exit := setupRoundTrip(t)
	cfg.Urls.Fetch = false
	cfg.Stats.Numcopies = 3
	flag.Chain = exit.address
	flag.Copies = 0
	msgFile := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(msgFile, []byte("To: alice@example.invalid\n\nHello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	flag.Args = []string{msgFile}
	cfg.Files.Pooldir = t.TempDir()
	mixprep()
	if n := len(poolFiles(t, cfg.Files.Pooldir, "m")); n != 3 {
		t.Fatalf("Expected 3 copies pooled, got %d", n)
	}
}
