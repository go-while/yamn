package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crooks/yamn/config"
	"github.com/crooks/yamn/keymgr"
	"github.com/dchest/blake2s"
)

// pingerKey returns a pubring.mix entry for a remailer whose public key is
// 32 bytes of seed, valid from yesterday for two weeks.
func pingerKey(name string, seed byte) (entry, keyid string) {
	pk := bytes.Repeat([]byte{seed}, 32)
	d, _ := blake2s.New(&blake2s.Config{Size: 16})
	d.Write(pk)
	keyid = hex.EncodeToString(d.Sum(nil))
	from := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	until := time.Now().UTC().AddDate(0, 0, 14).Format("2006-01-02")
	entry = fmt.Sprintf("%s %s@example.org %s 4:0.2.7 E %s %s\n\n"+
		"-----Begin Mix Key-----\n%s\n%s\n-----End Mix Key-----\n\n",
		name, name, keyid, from, until, keyid, hex.EncodeToString(pk))
	return
}

func pingerStats(lines ...string) string {
	s := "Stats-Version: 2.0\n" +
		"Generated: " + time.Now().UTC().Add(-time.Hour).Format("Mon 02 Jan 2006 15:04:05 GMT") + "\n" +
		"Mixmaster    Latent-Hist   Latent  Uptime-Hist   Uptime  Options\n" +
		"------------------------------------------------------------------------\n"
	for _, l := range lines {
		s += l + "\n"
	}
	return s + "\n"
}

// fakePinger serves a pubring.mix and mlist2.txt, or errors while down is set.
type fakePinger struct {
	*httptest.Server
	pubring, stats string
	down           atomic.Bool
}

func newFakePinger(t *testing.T, pubring, stats string) *fakePinger {
	p := &fakePinger{pubring: pubring, stats: stats}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.down.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		switch r.URL.Path {
		case "/pubring.mix":
			w.Write([]byte(p.pubring))
		case "/mlist2.txt":
			w.Write([]byte(p.stats))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *fakePinger) config(name string) config.Pinger {
	return config.Pinger{Name: name, Pubring: p.URL + "/pubring.mix", Mlist2: p.URL + "/mlist2.txt"}
}

// TestRefreshKeyringPingers fetches from three pingers, one of them lying
// about a key and one serving broken stats, and checks the merged files.
func TestRefreshKeyringPingers(t *testing.T) {
	setupTestConfig(t)
	alpha, _ := pingerKey("alpha", 1)
	beta, betaID := pingerKey("beta", 2)
	forged, _ := pingerKey("beta", 66)
	goodStats := pingerStats(
		"alpha        000000000000    :10   ++++++++++++  100.0%  D",
		"beta         000000000000    :20   ++++++++++++   99.0%  D",
	)
	a := newFakePinger(t, alpha+beta, goodStats)
	b := newFakePinger(t, alpha+beta, "<html>broken</html>")
	c := newFakePinger(t, alpha+forged, goodStats)

	dir := t.TempDir()
	cfg.Files.Pubring = filepath.Join(dir, "pubring.mix")
	cfg.Files.Mlist2 = filepath.Join(dir, "mlist2.txt")
	cfg.Urls.Fetch = true
	cfg.Urls.Pingers = []config.Pinger{
		a.config("a"), b.config("b"), c.config("c"),
		{Name: "../escape", Pubring: a.URL + "/pubring.mix"}, // ignored
		a.config("a"), // used twice: ignored
	}
	refreshKeyring(false)

	for _, f := range []string{"pubring.mix.a", "pubring.mix.b", "pubring.mix.c", "mlist2.txt.a", "mlist2.txt.c"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("pinger file not downloaded: %s", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "mlist2.txt.b")); err == nil {
		t.Error("invalid stats were stored")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); err == nil {
		t.Error("pinger name escaped the directory")
	}

	check := func() {
		t.Helper()
		p := keymgr.NewPubring(cfg.Files.Pubring, cfg.Files.Mlist2)
		if err := p.ImportPubring(); err != nil {
			t.Fatal(err)
		}
		if err := p.ImportStats(); err != nil {
			t.Fatal(err)
		}
		if p.Count() != 2 {
			t.Fatalf("merged keyring has %d keys, want 2", p.Count())
		}
		r, err := p.Get("beta")
		if err != nil {
			t.Fatal(err)
		}
		if got := hex.EncodeToString(r.Keyid); got != betaID {
			t.Errorf("beta has keyid %s, want the one two pingers agree on (%s)", got, betaID)
		}
		if n := len(p.Candidates(0, 60, 99, true)); n != 2 {
			t.Errorf("%d exit candidates with uptime >= 99%%, want 2", n)
		}
	}
	check()

	// A pinger that's down keeps its last copy, and an unchanged result
	// doesn't rewrite the merged keyring.
	a.down.Store(true)
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(cfg.Files.Pubring, old, old)
	refreshKeyring(true)
	check()
	if st, _ := os.Stat(cfg.Files.Pubring); !st.ModTime().Equal(old) {
		t.Error("unchanged merged keyring was rewritten")
	}
}

// TestRefreshKeyringSingle checks that without urls.pingers the single
// keyring and stats URLs are used, as before.
func TestRefreshKeyringSingle(t *testing.T) {
	setupTestConfig(t)
	alpha, _ := pingerKey("alpha", 1)
	a := newFakePinger(t, alpha, pingerStats("alpha        000000000000    :10   ++++++++++++  100.0%  D"))
	dir := t.TempDir()
	cfg.Files.Pubring = filepath.Join(dir, "pubring.mix")
	cfg.Files.Mlist2 = filepath.Join(dir, "mlist2.txt")
	cfg.Urls.Fetch = true
	cfg.Urls.Pubring = a.URL + "/pubring.mix"
	cfg.Urls.Mlist2 = a.URL + "/mlist2.txt"
	refreshKeyring(false)
	if got, _ := os.ReadFile(cfg.Files.Pubring); string(got) != alpha {
		t.Errorf("pubring not downloaded as is:\n%s", got)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.*.*"))
	if len(matches) > 0 {
		t.Errorf("unexpected per-pinger files: %v", matches)
	}

	// With urls.fetch off nothing is downloaded, unless forced (--refresh).
	os.Remove(cfg.Files.Pubring)
	cfg.Urls.Fetch = false
	refreshKeyring(false)
	if _, err := os.Stat(cfg.Files.Pubring); err == nil {
		t.Error("downloaded with urls.fetch off")
	}
	refreshKeyring(true)
	if _, err := os.Stat(cfg.Files.Pubring); err != nil {
		t.Error("--refresh didn't download")
	}
}

func TestUsablePingers(t *testing.T) {
	var names []string
	for _, p := range usablePingers([]config.Pinger{
		{Name: "mixmin"}, {Name: "victor-2"}, {Name: "a_b"},
		{Name: ""}, {Name: "../x"}, {Name: "a/b"}, {Name: "a.b"}, {Name: "mixmin"},
		{Name: "x123456789012345678901234567890123"},
	}) {
		names = append(names, p.Name)
	}
	if fmt.Sprint(names) != "[mixmin victor-2 a_b]" {
		t.Errorf("usable pingers: %v", names)
	}
}
