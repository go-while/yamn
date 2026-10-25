package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crooks/yamn/config"
	"github.com/crooks/yamn/keymgr"
	"github.com/crooks/yamn/minisign"
	"github.com/crooks/yamn/minisign/minisigntest"
)

// signedPinger serves files and their signatures, which tests change.
type signedPinger struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte // path → content, including .minisig files
}

func newSignedPinger(t *testing.T) *signedPinger {
	p := &signedPinger{files: make(map[string][]byte)}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		b, ok := p.files[r.URL.Path]
		p.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(p.Close)
	return p
}

// publish serves content as name, signed by k with the trusted comment
// trusted (no signature if k is nil).
func (p *signedPinger) publish(name string, content []byte, k *minisigntest.Key, trusted string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files["/"+name] = content
	delete(p.files, "/"+name+".minisig")
	if k != nil {
		p.files["/"+name+".minisig"] = k.Sign(content, trusted)
	}
}

func testKeys(t *testing.T, ks ...*minisigntest.Key) (pubs []minisign.PublicKey) {
	t.Helper()
	for _, k := range ks {
		p, err := minisign.ParsePublicKey(k.Public())
		if err != nil {
			t.Fatal(err)
		}
		pubs = append(pubs, p)
	}
	return
}

// TestFetchSigned checks which signed downloads are used.  A download that
// isn't keeps the previous copy.
func TestFetchSigned(t *testing.T) {
	setupTestConfig(t)
	k, other := minisigntest.NewKey(1), minisigntest.NewKey(2)
	// Keys with no signature accepted yet, so the rollback check can't
	// hide the check a case is about.
	expiredKey, futureKey, oldKey := minisigntest.NewKey(11), minisigntest.NewKey(12), minisigntest.NewKey(13)
	keys := testKeys(t, k, expiredKey, futureKey, oldKey)
	p := newSignedPinger(t)
	file := filepath.Join(t.TempDir(), "pubring.mix")
	url := p.URL + "/pubring.mix"
	now := time.Now()
	ring := func(seed byte) []byte {
		e, _ := pingerKey("alpha", seed)
		return []byte(e)
	}
	comment := func(name string, ts time.Time, valid time.Duration) string {
		return minisigntest.Comment(name, ts, valid)
	}

	// fetch publishes content signed by sk with trusted, fetches it and
	// checks whether it replaced the local file.
	seed := byte(10)
	fetch := func(desc string, sk *minisigntest.Key, trusted string, want bool) {
		t.Helper()
		seed++
		content := ring(seed)
		p.publish("pubring.mix", content, sk, trusted)
		before, _ := os.ReadFile(file)
		err := fetchFile(url, file, keymgr.CheckPubring, keys)
		after, _ := os.ReadFile(file)
		switch {
		case want && err != nil:
			t.Errorf("%s: %v", desc, err)
		case want && string(after) != string(content):
			t.Errorf("%s: file not replaced", desc)
		case !want && err == nil:
			t.Errorf("%s: accepted", desc)
		case !want && string(after) != string(before):
			t.Errorf("%s: file replaced although rejected", desc)
		}
	}

	fetch("signed", k, comment("pubring.mix", now.Add(-time.Hour), 48*time.Hour), true)
	fetch("no signature", nil, "", false)
	fetch("other key", other, comment("pubring.mix", now, 48*time.Hour), false)
	fetch("another file's signature", k, comment("mlist2.txt", now, 48*time.Hour), false)
	fetch("expired", expiredKey, comment("pubring.mix", now.Add(-3*time.Hour), 2*time.Hour), false)
	fetch("an hour in the future", futureKey, comment("pubring.mix", now.Add(time.Hour), 48*time.Hour), false)
	fetch("older than max_signature_age_hours", oldKey, comment("pubring.mix", now.Add(-50*time.Hour), 52*time.Hour), false)
	fetch("10 minutes ahead", k, comment("pubring.mix", now.Add(10*time.Minute), 48*time.Hour), true)

	// Rollback: an older signature than the last accepted one.
	fetch("older than the last", k, comment("pubring.mix", now.Add(-time.Minute), 48*time.Hour), false)
	fetch("same timestamp again", k, comment("pubring.mix", now.Add(10*time.Minute), 48*time.Hour), true)
	// The record is per key: a new key starts afresh.
	keys = append(keys, testKeys(t, other)...)
	fetch("new key, older timestamp", other, comment("pubring.mix", now.Add(-time.Hour), 48*time.Hour), true)
	// The rejected signatures left no record.
	state, err := loadSigState(cfg.Files.Sigstate)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(10 * time.Minute).Unix()
	if got := state[sigStateID(keys[0].ID, "pubring.mix")]; got != want {
		t.Errorf("state for the first key: %d, want %d", got, want)
	}
	if len(state) != 2 {
		t.Errorf("state %v, want two entries", state)
	}

	// The signature of the previous file (fetched between the pinger's
	// two writes).
	old := k.Sign(ring(seed), comment("pubring.mix", now.Add(15*time.Minute), 48*time.Hour))
	p.publish("pubring.mix", ring(99), nil, "")
	p.mu.Lock()
	p.files["/pubring.mix.minisig"] = old
	p.mu.Unlock()
	if err := fetchFile(url, file, keymgr.CheckPubring, keys); err == nil {
		t.Error("signature of another content accepted")
	}

	// A signed file must still pass the validator.
	html := []byte("<html>signed but not a keyring</html>\n")
	p.publish("pubring.mix", html, k, comment("pubring.mix", now.Add(20*time.Minute), 48*time.Hour))
	if err := fetchFile(url, file, keymgr.CheckPubring, keys); err == nil {
		t.Error("signed HTML page accepted")
	}

	// max_signature_age_hours 0: only the expiry counts.
	cfg.Urls.MaxSignatureAge = 0
	keys = testKeys(t, minisigntest.NewKey(3))
	fetch("old, max age off", minisigntest.NewKey(3), comment("pubring.mix", now.Add(-100*time.Hour), 101*time.Hour), true)

	// A damaged state file refuses signed downloads rather than forgetting.
	if err := os.WriteFile(cfg.Files.Sigstate, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	fetch("damaged state", minisigntest.NewKey(3), comment("pubring.mix", now, 48*time.Hour), false)
}

// TestFetchSignedVectors fetches go-echolot's test vectors at the time
// they were signed.
func TestFetchSignedVectors(t *testing.T) {
	setupTestConfig(t)
	pub, err := os.ReadFile("minisign/testdata/pinger.pub")
	if err != nil {
		t.Fatal(err)
	}
	keys := pingerKeys([]string{strings.Split(string(pub), "\n")[1]})
	if len(keys) != 1 {
		t.Fatal("test vector key not parsed")
	}
	srv := httptest.NewServer(http.FileServer(http.Dir("minisign/testdata")))
	defer srv.Close()
	signed := time.Unix(1792670400, 0)
	for _, f := range []string{"pubring.mix", "mlist2.txt"} {
		content, _ := os.ReadFile(filepath.Join("minisign/testdata", f))
		accept, err := checkSignature(srv.URL+"/"+f, content, keys, cfg.Files.Sigstate, signed.Add(time.Hour))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if err := accept(filepath.Join(t.TempDir(), f)); err != nil {
			t.Error(err)
		}
		if _, err := checkSignature(srv.URL+"/"+f, content, keys, cfg.Files.Sigstate, signed.Add(49*time.Hour)); err == nil {
			t.Errorf("%s: accepted after expiry", f)
		}
	}
}

func TestSignatureURL(t *testing.T) {
	for in, want := range map[string][2]string{
		"https://example.org/yamn/pubring.mix":    {"https://example.org/yamn/pubring.mix.minisig", "pubring.mix"},
		"https://example.org/yamn/mlist2.txt?x=1": {"https://example.org/yamn/mlist2.txt.minisig?x=1", "mlist2.txt"},
		"http://abc.onion/pubring.mix#frag":       {"http://abc.onion/pubring.mix.minisig#frag", "pubring.mix"},
		"https://example.org/a%20b/pubring.mix":   {"https://example.org/a%20b/pubring.mix.minisig", "pubring.mix"},
	} {
		sig, name, err := signatureURL(in)
		if err != nil || sig != want[0] || name != want[1] {
			t.Errorf("%s: %q %q %v, want %q %q", in, sig, name, err, want[0], want[1])
		}
	}
	for _, bad := range []string{"https://example.org", "https://example.org/", "::"} {
		if _, _, err := signatureURL(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// TestRefreshKeyringSigned checks that refreshKeyring passes each pinger's
// keys: a pinger whose files are signed by another key doesn't count.
func TestRefreshKeyringSigned(t *testing.T) {
	setupTestConfig(t)
	ka, kb := minisigntest.NewKey(1), minisigntest.NewKey(2)
	alpha, _ := pingerKey("alpha", 1)
	beta, _ := pingerKey("beta", 2)
	stats := []byte(pingerStats(
		"alpha        000000000000    :10   ++++++++++++  100.0%  D",
		"beta         000000000000    :20   ++++++++++++   99.0%  D",
	))
	now := time.Now()
	a, b := newSignedPinger(t), newSignedPinger(t)
	for _, s := range []struct {
		p    *signedPinger
		ring string
		k    *minisigntest.Key
	}{{a, alpha + beta, ka}, {b, alpha, ka}} { // b signs with a's key, not its own
		s.p.publish("pubring.mix", []byte(s.ring), s.k, minisigntest.Comment("pubring.mix", now, 48*time.Hour))
		s.p.publish("mlist2.txt", stats, s.k, minisigntest.Comment("mlist2.txt", now, 6*time.Hour))
	}
	cfg.Urls.KeyQuorum = 1
	cfg.Urls.Pingers = []config.Pinger{
		{Name: "a", Pubring: a.URL + "/pubring.mix", Mlist2: a.URL + "/mlist2.txt", Keys: []string{ka.Public()}},
		{Name: "b", Pubring: b.URL + "/pubring.mix", Mlist2: b.URL + "/mlist2.txt", Keys: []string{kb.Public()}},
	}
	refreshKeyring(true)
	if _, err := os.Stat(pingerFile(cfg.Files.Pubring, "a")); err != nil {
		t.Errorf("pinger a's keyring not stored: %v", err)
	}
	if _, err := os.Stat(pingerFile(cfg.Files.Pubring, "b")); err == nil {
		t.Error("pinger b's keyring, signed by another key, stored")
	}

	// A single pinger with urls.pinger_keys.
	cfg.Urls.Pingers = nil
	cfg.Urls.PingerKeys = []string{kb.Public()}
	cfg.Urls.Pubring, cfg.Urls.Mlist2 = a.URL+"/pubring.mix", a.URL+"/mlist2.txt"
	os.Remove(cfg.Files.Pubring)
	refreshKeyring(true)
	if _, err := os.Stat(cfg.Files.Pubring); err == nil {
		t.Error("keyring signed by a key not in pinger_keys stored")
	}
	cfg.Urls.PingerKeys = []string{ka.Public()}
	refreshKeyring(true)
	if _, err := os.Stat(cfg.Files.Pubring); err != nil {
		t.Errorf("keyring signed by pinger_keys not stored: %v", err)
	}
}

// TestSignedOnly checks when a pinger's stored copy counts: only while the
// signature stored next to it verifies, names the file and is neither
// expired nor older than urls.max_signature_age_hours.
func TestSignedOnly(t *testing.T) {
	setupTestConfig(t)
	k, other := minisigntest.NewKey(5), minisigntest.NewKey(6)
	keys := testKeys(t, k)
	content := []byte("keyring\n")
	now := time.Now()
	dir := t.TempDir()
	url := "https://pinger.example/yamn/pubring.mix"
	for _, c := range []struct {
		name   string
		sig    []byte // nil: no signature stored
		data   []byte
		maxAge int
		ok     bool
	}{
		{"valid", k.Sign(content, minisigntest.Comment("pubring.mix", now.Add(-time.Hour), 48*time.Hour)), content, 48, true},
		{"no signature", nil, content, 48, false},
		{"other key", other.Sign(content, minisigntest.Comment("pubring.mix", now, 48*time.Hour)), content, 48, false},
		{"changed file", k.Sign(content, minisigntest.Comment("pubring.mix", now, 48*time.Hour)), []byte("changed\n"), 48, false},
		{"other file name", k.Sign(content, minisigntest.Comment("mlist2.txt", now, 48*time.Hour)), content, 48, false},
		{"expired", k.Sign(content, minisigntest.Comment("pubring.mix", now.Add(-7*time.Hour), 6*time.Hour)), content, 48, false},
		{"too old", k.Sign(content, minisigntest.Comment("pubring.mix", now.Add(-50*time.Hour), 72*time.Hour)), content, 48, false},
		{"old, no age limit", k.Sign(content, minisigntest.Comment("pubring.mix", now.Add(-50*time.Hour), 72*time.Hour)), content, 0, true},
	} {
		cfg.Urls.MaxSignatureAge = c.maxAge
		file := filepath.Join(dir, strings.ReplaceAll(c.name, " ", "-"))
		os.Remove(file + signatureSuffix)
		if c.sig != nil {
			if err := os.WriteFile(file+signatureSuffix, c.sig, 0600); err != nil {
				t.Fatal(err)
			}
		}
		got := signedOnly("p", file, url, c.data, keys, now)
		if (got != nil) != c.ok {
			t.Errorf("%s: used %v, want %v", c.name, got != nil, c.ok)
		}
	}
	if signedOnly("p", filepath.Join(dir, "missing"), url, nil, keys, now) != nil {
		t.Error("a missing file counts")
	}
}

// TestRefreshKeyringStaleSigned checks that a signing pinger whose
// downloads are refused stops counting once its stored signature expires,
// instead of voting with its last accepted keyring until the keys expire.
func TestRefreshKeyringStaleSigned(t *testing.T) {
	setupTestConfig(t)
	alpha, _ := pingerKey("alpha", 1)
	beta, _ := pingerKey("beta", 2)
	stats := []byte(pingerStats(
		"alpha        000000000000    :10   ++++++++++++  100.0%  D",
		"beta         000000000000    :20   ++++++++++++   99.0%  D",
	))
	now := time.Now()
	type pinger struct {
		name string
		srv  *signedPinger
		key  *minisigntest.Key
		ring string
	}
	// beta reaches the quorum of 2 only with a and c.
	ps := []*pinger{
		{name: "a", srv: newSignedPinger(t), key: minisigntest.NewKey(11), ring: alpha + beta},
		{name: "b", srv: newSignedPinger(t), key: minisigntest.NewKey(12), ring: alpha},
		{name: "c", srv: newSignedPinger(t), key: minisigntest.NewKey(13), ring: alpha + beta},
	}
	cfg.Urls.KeyQuorum = 2
	cfg.Urls.Pingers = nil
	for _, p := range ps {
		p.srv.publish("pubring.mix", []byte(p.ring), p.key, minisigntest.Comment("pubring.mix", now, 48*time.Hour))
		p.srv.publish("mlist2.txt", stats, p.key, minisigntest.Comment("mlist2.txt", now, 6*time.Hour))
		cfg.Urls.Pingers = append(cfg.Urls.Pingers, config.Pinger{
			Name: p.name, Pubring: p.srv.URL + "/pubring.mix", Mlist2: p.srv.URL + "/mlist2.txt",
			Keys: []string{p.key.Public()},
		})
	}
	merged := func() string {
		b, err := os.ReadFile(cfg.Files.Pubring)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	refreshKeyring(true)
	if !strings.Contains(merged(), "beta@example.org") {
		t.Fatal("beta, published by a and c, not merged")
	}
	for _, p := range ps {
		if _, err := os.Stat(pingerFile(cfg.Files.Pubring, p.name) + signatureSuffix); err != nil {
			t.Errorf("pinger %s: signature not stored: %v", p.name, err)
		}
	}

	// c stops signing: its downloads are refused and its copy stays.  While
	// the stored signature holds, c still counts.
	c := ps[2]
	c.srv.publish("pubring.mix", []byte(c.ring), nil, "")
	c.srv.publish("mlist2.txt", stats, nil, "")
	refreshKeyring(true)
	if !strings.Contains(merged(), "beta@example.org") {
		t.Error("beta dropped while c's stored signature still holds")
	}

	// Time passes: c's stored signature has expired, so c no longer counts
	// and beta, now published by a alone, is dropped.
	cFile := pingerFile(cfg.Files.Pubring, c.name)
	expired := c.key.Sign([]byte(c.ring), minisigntest.Comment("pubring.mix", now.Add(-49*time.Hour), 48*time.Hour))
	if err := os.WriteFile(cFile+signatureSuffix, expired, 0644); err != nil {
		t.Fatal(err)
	}
	refreshKeyring(true)
	if strings.Contains(merged(), "beta@example.org") {
		t.Error("beta still merged with the vote of c, whose signature has expired")
	}
	if !strings.Contains(merged(), "alpha@example.org") {
		t.Error("alpha, published by a and b, dropped")
	}
}
