package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestFetchOnSend checks the documented setup for keeping keyring downloads
// apart from sending: with urls.fetch true the client downloads old files
// right before sending; with false it doesn't, and --refresh still does.
func TestFetchOnSend(t *testing.T) {
	_, exit := setupRoundTrip(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r) // The old files are kept
	}))
	defer srv.Close()
	cfg.Urls.Pubring = srv.URL + "/pubring.mix"
	cfg.Urls.Mlist2 = srv.URL + "/mlist2.txt"
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(cfg.Files.Pubring, old, old); err != nil {
		t.Fatal(err)
	}
	flag.Chain = exit.address
	flag.Copies = 1
	msgFile := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(msgFile, []byte("To: alice@example.invalid\n\nHello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	flag.Args = []string{msgFile}

	for _, fetch := range []bool{true, false} {
		cfg.Urls.Fetch = fetch
		cfg.Files.Pooldir = t.TempDir()
		hits.Store(0)
		mixprep()
		if got := hits.Load() > 0; got != fetch {
			t.Errorf("urls.fetch %v: downloaded at send time %v", fetch, got)
		}
		if n := len(poolFiles(t, cfg.Files.Pooldir, "m")); n != 1 {
			t.Errorf("urls.fetch %v: %d packets pooled, want 1", fetch, n)
		}
	}

	hits.Store(0)
	refreshKeyring(true) // --refresh
	if hits.Load() == 0 {
		t.Error("--refresh with urls.fetch false didn't download")
	}
}
