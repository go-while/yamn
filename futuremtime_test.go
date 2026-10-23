package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crooks/yamn/keymgr"
)

// TestFetchFutureMtime checks that a keyring dated in the future (tar and
// zip keep mtimes) counts as old and is fetched again, while a fresh one
// isn't.
func TestFetchFutureMtime(t *testing.T) {
	setupTestConfig(t)
	cfg.Urls.Fetch = true
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	f := filepath.Join(t.TempDir(), "pubring.mix")
	if err := os.WriteFile(f, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		mtime time.Time
		fetch bool
	}{
		{"fresh", time.Now(), false},
		{"two hours old", time.Now().Add(-2 * time.Hour), true},
		{"a year in the future", time.Now().AddDate(1, 0, 0), true},
	} {
		if err := os.Chtimes(f, tt.mtime, tt.mtime); err != nil {
			t.Fatal(err)
		}
		hits.Store(0)
		timedURLFetch(srv.URL, f, keymgr.CheckPubring, nil)
		if got := hits.Load() > 0; got != tt.fetch {
			t.Errorf("%s: fetched %v, want %v", tt.name, got, tt.fetch)
		}
	}
}
