package idlog

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestIDLog(t *testing.T) (*IDLog, string) {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "idlog")
	i, err := NewIDLog(filename, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(i.Close)
	return i, filename
}

func TestNewIDLogInvalidDays(t *testing.T) {
	for _, days := range []int{0, -1} {
		if _, err := NewIDLog(filepath.Join(t.TempDir(), "idlog"), days); err == nil {
			t.Errorf("NewIDLog with %d days: no error", days)
		}
	}
}

// TestNewIDLogLocked checks that an ID log open elsewhere gives an error,
// not a panic.
func TestNewIDLogLocked(t *testing.T) {
	_, filename := newTestIDLog(t)
	if i, err := NewIDLog(filename, 1); err == nil {
		i.Close()
		t.Error("a second open succeeded")
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
	i, err := NewIDLog(filename, 1)
	if err != nil {
		t.Fatal(err)
	}
	i.Unique([]byte("packet-1"))
	i.Close()

	if i, err = NewIDLog(filename, 1); err != nil {
		t.Fatal(err)
	}
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

	count, deleted, bad := i.Expire()
	if count != 1 || deleted != 1 || bad != 0 {
		t.Fatalf("Expire: expected count=1, deleted=1, bad=0, got count=%d, deleted=%d, bad=%d", count, deleted, bad)
	}
	if !i.Unique([]byte("old")) {
		t.Fatal("Expired key should have been deleted")
	}
	if i.Unique([]byte("new")) {
		t.Fatal("Unexpired key should have been kept")
	}
}

// TestExpireBadValue checks that an unreadable entry doesn't stop Expire,
// which runs at every startup, and that its packet ID stays known.
func TestExpireBadValue(t *testing.T) {
	i, _ := newTestIDLog(t)
	i.Unique([]byte("good"))
	if err := i.db.Put([]byte("corrupt"), []byte("not a gob time"), nil); err != nil {
		t.Fatal(err)
	}
	var count, deleted, bad int
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Expire panics on a bad entry: %v", r)
			}
		}()
		count, deleted, bad = i.Expire()
	}()
	if count != 2 || deleted != 0 || bad != 1 {
		t.Fatalf("Expire: expected count=2, deleted=0, bad=1, got %d, %d, %d", count, deleted, bad)
	}
	if i.Unique([]byte("corrupt")) {
		t.Fatal("the packet ID of the bad entry was forgotten")
	}
	// The entry was repaired: the next run can read it.
	if _, _, bad = i.Expire(); bad != 0 {
		t.Fatalf("bad entry not repaired: bad=%d", bad)
	}
}
