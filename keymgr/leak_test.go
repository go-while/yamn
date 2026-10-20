package keymgr

import (
	"os"
	"runtime/debug"
	"testing"
)

// openFDs returns the number of open file descriptors of this process.
func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("needs /proc/self/fd")
	}
	return len(entries)
}

// TestImportClosesFiles checks that reading the pubring and stats files
// doesn't leave them open.  The garbage collector is paused, as it would
// otherwise close leaked files at random through finalizers.
func TestImportClosesFiles(t *testing.T) {
	pubring, mlist2 := writeTestFiles(t)
	p := NewPubring(pubring, mlist2)
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	before := openFDs(t)
	for range 20 {
		if err := p.ImportPubring(); err != nil {
			t.Fatal(err)
		}
		if err := p.ImportStats(); err != nil {
			t.Fatal(err)
		}
		if _, err := Headers(pubring); err != nil {
			t.Fatal(err)
		}
	}
	if leaked := openFDs(t) - before; leaked > 0 {
		t.Fatalf("%d file descriptors left open after 20 imports", leaked)
	}
}
