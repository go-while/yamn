package idlog

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestIDLog(t *testing.T) (*IDLog, string) {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "idlog")
	i := NewIDLog(filename, 1)
	t.Cleanup(i.Close)
	return i, filename
}

func TestNewIDLogInvalidDays(t *testing.T) {
	for _, days := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewIDLog with %d days should panic", days)
				}
			}()
			NewIDLog(filepath.Join(t.TempDir(), "idlog"), days)
		}()
	}
}

func TestUnique(t *testing.T) {
	i, _ := newTestIDLog(t)
	if !i.Unique([]byte("packet-1")) {
		t.Fatal("First sighting of a key should be unique")
	}
	if i.Unique([]byte("packet-1")) {
		t.Fatal("Second sighting of a key should not be unique")
	}
	if !i.Unique([]byte("packet-2")) {
		t.Fatal("A different key should be unique")
	}
}

func TestForget(t *testing.T) {
	i, _ := newTestIDLog(t)
	i.Unique([]byte("packet-1"))
	i.Forget([]byte("packet-1"))
	if !i.Unique([]byte("packet-1")) {
		t.Fatal("A forgotten key should be unique again")
	}
}

// TestUniquePersists checks that replay protection survives a restart.
func TestUniquePersists(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "idlog")
	i := NewIDLog(filename, 1)
	i.Unique([]byte("packet-1"))
	i.Close()

	i = NewIDLog(filename, 1)
	defer i.Close()
	if i.Unique([]byte("packet-1")) {
		t.Fatal("Key should still be known after reopening the ID log")
	}
}

func TestExpire(t *testing.T) {
	i, _ := newTestIDLog(t)
	// Insert one key that has already expired and one that has not.
	validity := i.validity
	i.validity = -time.Hour
	i.Unique([]byte("old"))
	i.validity = validity
	i.Unique([]byte("new"))

	count, deleted := i.Expire()
	if count != 1 || deleted != 1 {
		t.Fatalf("Expire: expected count=1, deleted=1, got count=%d, deleted=%d", count, deleted)
	}
	if !i.Unique([]byte("old")) {
		t.Fatal("Expired key should have been deleted")
	}
	if i.Unique([]byte("new")) {
		t.Fatal("Unexpired key should have been kept")
	}
}
