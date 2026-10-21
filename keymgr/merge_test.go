package keymgr

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// mergeNow is the time all merge tests run at.
var mergeNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// testKey describes one key in a test pubring.
type testKey struct {
	name, addr string
	seed       byte   // the public key is 32 bytes of seed
	version    string // default 4:0.2.7
	caps       string // default E
	from, to   string // default 2026-10-15 and 2026-10-29
}

func (k testKey) pk() []byte { return bytes.Repeat([]byte{k.seed}, 32) }

func (k testKey) keyid() string { return hex.EncodeToString(makeKeyID(k.pk())) }

func testPubring(keys ...testKey) []byte {
	var b strings.Builder
	for _, k := range keys {
		version, caps, from, to := k.version, k.caps, k.from, k.to
		if version == "" {
			version = "4:0.2.7"
		}
		if caps == "" {
			caps = "E"
		}
		if from == "" {
			from = "2026-10-15"
		}
		if to == "" {
			to = "2026-10-29"
		}
		fmt.Fprintf(&b, "%s %s %s %s %s %s %s\n\n", k.name, k.addr, k.keyid(), version, caps, from, to)
		fmt.Fprintf(&b, "-----Begin Mix Key-----\n%s\n%s\n-----End Mix Key-----\n\n",
			k.keyid(), hex.EncodeToString(k.pk()))
	}
	return []byte(b.String())
}

// testStats returns an mlist2.txt generated age ago with rows of
// "name latency-minutes uptime-percent".
func testStats(age time.Duration, rows ...string) []byte {
	var b strings.Builder
	b.WriteString("Stats-Version: 2.0\n")
	fmt.Fprintf(&b, "Generated: %s\n", mergeNow.Add(-age).Format(generatedFormat))
	b.WriteString("Mixmaster    Latent-Hist   Latent  Uptime-Hist   Uptime  Options\n")
	b.WriteString(strings.Repeat("-", 72) + "\n")
	for _, r := range rows {
		var name string
		var lat int
		var up float64
		fmt.Sscan(r, &name, &lat, &up)
		fmt.Fprintf(&b, "%-12s 000000000000 %2d:%02d   ++++++++++++  %5.1f%%  D\n", name, lat/60, lat%60, up)
	}
	b.WriteString("\nRemailer-Capabilities:\n\n")
	return []byte(b.String())
}

var (
	alpha    = testKey{name: "alpha", addr: "alpha@example.org", seed: 1}
	beta     = testKey{name: "beta", addr: "beta@example.org", seed: 2}
	gamma    = testKey{name: "gamma", addr: "gamma@example.org", seed: 3}
	alphaNew = testKey{name: "alpha", addr: "alpha@example.org", seed: 11, from: "2026-10-20", to: "2026-11-03"}
)

// mergedKeys returns "name keyid" for every key in a merged pubring.
func mergedKeys(t *testing.T, res MergeResult) (keys []string) {
	t.Helper()
	if res.Pubring == nil {
		t.Fatalf("no merged pubring; warnings: %v", res.Warnings)
	}
	rems, err := readPubring(bytes.NewReader(res.Pubring), mergeNow, false, func(format string, a ...any) {
		t.Errorf("merged pubring: "+format, a...)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rems {
		keys = append(keys, r.name+" "+hex.EncodeToString(r.Keyid))
	}
	return
}

func want(keys ...testKey) (w []string) {
	for _, k := range keys {
		w = append(w, k.name+" "+k.keyid())
	}
	return
}

func checkKeys(t *testing.T, res MergeResult, keys ...testKey) {
	t.Helper()
	got, exp := mergedKeys(t, res), want(keys...)
	if !slices.Equal(got, exp) {
		t.Errorf("merged keys:\n got %v\nwant %v\nwarnings: %v\nnotes: %v", got, exp, res.Warnings, res.Notes)
	}
}

func merge(quorum int, pingers ...PingerFiles) MergeResult {
	return MergePingers(pingers, quorum, 24, false, mergeNow)
}

func TestMergeAgreement(t *testing.T) {
	ring := testPubring(alpha, beta, gamma)
	res := merge(2,
		PingerFiles{Name: "a", Pubring: ring},
		PingerFiles{Name: "b", Pubring: ring},
		PingerFiles{Name: "c", Pubring: ring},
	)
	checkKeys(t, res, alpha, beta, gamma)
	if res.Quorum != 2 {
		t.Errorf("quorum %d, want 2", res.Quorum)
	}
	// No stats were given; that's the only thing to warn about.
	if len(res.Notes) > 0 || !slices.Equal(res.Warnings, []string{"no pinger has usable stats"}) {
		t.Errorf("unexpected messages: %v %v", res.Warnings, res.Notes)
	}
	if res.Stats != nil {
		t.Error("merged stats without any input stats")
	}
	// Agreeing pingers produce the same file as their own.
	if !bytes.Equal(res.Pubring, ring) {
		t.Errorf("merged pubring differs from the input:\n%s", res.Pubring)
	}
}

// TestMergeOneLyingPinger checks that one pinger can't replace a key or add
// a remailer.
func TestMergeOneLyingPinger(t *testing.T) {
	forged := testKey{name: "beta", addr: "beta@example.org", seed: 66}
	evil := testKey{name: "evil", addr: "evil@example.org", seed: 99}
	res := merge(2,
		PingerFiles{Name: "a", Pubring: testPubring(alpha, beta)},
		PingerFiles{Name: "b", Pubring: testPubring(alpha, beta)},
		PingerFiles{Name: "c", Pubring: testPubring(alpha, forged, evil)},
	)
	checkKeys(t, res, alpha, beta)
	joined := strings.Join(res.Notes, "\n")
	if !strings.Contains(joined, "beta@example.org: pingers disagree") {
		t.Errorf("disagreement on beta not reported: %v", res.Notes)
	}
	if !strings.Contains(joined, "evil@example.org: no key is published by 2 pingers") {
		t.Errorf("rejected key not reported: %v", res.Notes)
	}
}

func TestMergeRotation(t *testing.T) {
	// Two pingers already have the new key: it's used.
	res := merge(2,
		PingerFiles{Name: "a", Pubring: testPubring(alphaNew, beta)},
		PingerFiles{Name: "b", Pubring: testPubring(alphaNew, beta)},
		PingerFiles{Name: "c", Pubring: testPubring(alpha, beta)},
	)
	checkKeys(t, res, alphaNew, beta)

	// Only one has it: the old key, still valid, stays in use.
	res = merge(2,
		PingerFiles{Name: "a", Pubring: testPubring(alphaNew, beta)},
		PingerFiles{Name: "b", Pubring: testPubring(alpha, beta)},
		PingerFiles{Name: "c", Pubring: testPubring(alpha, beta)},
	)
	checkKeys(t, res, alpha, beta)

	// With a quorum of one both keys qualify; the newer one wins.
	res = merge(1,
		PingerFiles{Name: "a", Pubring: testPubring(alpha, beta)},
		PingerFiles{Name: "b", Pubring: testPubring(alphaNew, beta)},
	)
	checkKeys(t, res, alphaNew, beta)
}

// TestMergeQuorumShrinks checks that with fewer usable keyrings than the
// quorum, all of them must agree, and that it's reported.
func TestMergeQuorumShrinks(t *testing.T) {
	res := merge(2,
		PingerFiles{Name: "a", Pubring: testPubring(alpha, beta)},
		PingerFiles{Name: "b", Pubring: []byte("<html>not a keyring</html>")},
		PingerFiles{Name: "c"},
	)
	checkKeys(t, res, alpha, beta)
	if res.Quorum != 1 {
		t.Errorf("quorum %d, want 1", res.Quorum)
	}
	w := strings.Join(res.Warnings, "\n")
	if !strings.Contains(w, "b: keyring unusable") || !strings.Contains(w, "only 1 pinger(s)") {
		t.Errorf("missing warnings: %v", res.Warnings)
	}

	res = merge(2, PingerFiles{Name: "a"}, PingerFiles{Name: "b", Pubring: []byte("junk")})
	if res.Pubring != nil || res.Stats != nil {
		t.Error("merged files without any usable keyring")
	}
}

func TestMergeNameConflict(t *testing.T) {
	impostor := testKey{name: "alpha", addr: "impostor@example.org", seed: 77}
	// The real alpha has more votes.
	res := merge(2,
		PingerFiles{Name: "a", Pubring: testPubring(alpha, beta)},
		PingerFiles{Name: "b", Pubring: testPubring(alpha, impostor, beta)},
		PingerFiles{Name: "c", Pubring: testPubring(alpha, impostor, beta)},
	)
	checkKeys(t, res, alpha, beta)
	// A tie: neither can be trusted.
	res = merge(2,
		PingerFiles{Name: "a", Pubring: testPubring(alpha, beta)},
		PingerFiles{Name: "b", Pubring: testPubring(alpha, impostor, beta)},
		PingerFiles{Name: "c", Pubring: testPubring(impostor, beta)},
	)
	checkKeys(t, res, beta)
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "name alpha is used by several addresses") {
		t.Errorf("name conflict not reported: %v", res.Warnings)
	}
}

func TestMergeExpiredKeys(t *testing.T) {
	old := testKey{name: "old", addr: "old@example.org", seed: 5, from: "2026-09-01", to: "2026-10-15"}
	ring := testPubring(alpha, old)
	pingers := []PingerFiles{{Name: "a", Pubring: ring}, {Name: "b", Pubring: ring}}
	checkKeys(t, MergePingers(pingers, 2, 24, false, mergeNow), alpha)
	res := MergePingers(pingers, 2, 24, true, mergeNow)
	rems, _ := readPubring(bytes.NewReader(res.Pubring), mergeNow, true, func(string, ...any) {})
	if len(rems) != 2 {
		t.Errorf("use_expired: got %d keys, want 2", len(rems))
	}
}

// TestMergeHeaderMajority checks that the header fields most pingers
// publish are used.
func TestMergeHeaderMajority(t *testing.T) {
	older := alpha
	older.version, older.caps = "4:0.2.6", "M"
	res := merge(2,
		PingerFiles{Name: "a", Pubring: testPubring(older)},
		PingerFiles{Name: "b", Pubring: testPubring(alpha)},
		PingerFiles{Name: "c", Pubring: testPubring(alpha)},
	)
	if !bytes.Contains(res.Pubring, []byte(" 4:0.2.7 E ")) {
		t.Errorf("majority header not used:\n%s", res.Pubring)
	}
}

func TestMergeStats(t *testing.T) {
	ring := testPubring(alpha, beta, gamma)
	res := merge(2,
		PingerFiles{Name: "a", Pubring: ring, Stats: testStats(1*time.Hour, "alpha 10 100.0", "beta 30 99.0")},
		PingerFiles{Name: "b", Pubring: ring, Stats: testStats(2*time.Hour, "alpha 20 98.0", "beta 50 97.0", "gamma 75 90.0")},
		PingerFiles{Name: "c", Pubring: ring, Stats: testStats(3*time.Hour, "alpha 40 99.5")},
		// Stale stats are ignored.
		PingerFiles{Name: "d", Pubring: ring, Stats: testStats(30*time.Hour, "alpha 600 10.0", "beta 600 10.0")},
	)
	if res.Stats == nil {
		t.Fatalf("no merged stats: %v", res.Warnings)
	}
	dir := t.TempDir()
	pubFile, statsFile := filepath.Join(dir, "pubring.mix"), filepath.Join(dir, "mlist2.txt")
	os.WriteFile(pubFile, res.Pubring, 0644)
	os.WriteFile(statsFile, res.Stats, 0644)
	p := NewPubring(pubFile, statsFile)
	if err := p.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	if err := p.ImportStats(); err != nil {
		t.Fatalf("merged stats don't import: %s\n%s", err, res.Stats)
	}
	for _, c := range []struct {
		addr           string
		latent, uptime int
	}{
		{"alpha@example.org", 20, 995}, // median of 3
		{"beta@example.org", 50, 970},  // 2 values: the worse of each
		{"gamma@example.org", 75, 900}, // only one pinger has stats
	} {
		r := p.pub[c.addr]
		if r.latent != c.latent || r.uptime != c.uptime {
			t.Errorf("%s: latency %d uptime %d, want %d %d", c.addr, r.latent, r.uptime, c.latent, c.uptime)
		}
	}
	// The oldest source used sets the Generated time.
	if wantGen := mergeNow.Add(-3 * time.Hour); !p.statsGenerated.Equal(wantGen) {
		t.Errorf("Generated %s, want %s", p.statsGenerated, wantGen)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "d: stats are 30h0m0s old, not used") {
		t.Errorf("stale stats not reported: %v", res.Notes)
	}
	// Latencies over an hour use the hh:mm form.
	if !bytes.Contains(res.Stats, []byte(" 1:15   ")) {
		t.Errorf("gamma's latency not formatted as 1:15:\n%s", res.Stats)
	}
}

// TestMergeStaleStatsFallback checks that old stats are used when there are
// no fresh ones, keeping their age so the client warns about it.
func TestMergeStaleStatsFallback(t *testing.T) {
	ring := testPubring(alpha)
	res := merge(1, PingerFiles{Name: "a", Pubring: ring, Stats: testStats(48*time.Hour, "alpha 10 99.0")})
	if res.Stats == nil {
		t.Fatal("stale stats dropped")
	}
	if !bytes.Contains(res.Stats, []byte(mergeNow.Add(-48*time.Hour).Format(generatedFormat))) {
		t.Errorf("Generated not kept:\n%s", res.Stats)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "older than 24 hours") {
		t.Errorf("no warning: %v", res.Warnings)
	}
}

// TestMergeRealPubrings merges the format of a real Echolot pubring: the
// output must be what yamn reads, key for key.
func TestMergeRealPubrings(t *testing.T) {
	ring := []byte(fakePubring())
	res := MergePingers([]PingerFiles{{Name: "a", Pubring: ring}, {Name: "b", Pubring: ring}}, 2, 24, false, time.Now())
	dir := t.TempDir()
	orig, merged := filepath.Join(dir, "orig.mix"), filepath.Join(dir, "merged.mix")
	os.WriteFile(orig, ring, 0644)
	os.WriteFile(merged, res.Pubring, 0644)
	a, b := NewPubring(orig, ""), NewPubring(merged, "")
	a.ImportPubring()
	b.ImportPubring()
	ka, kb := a.KeyList(), b.KeyList()
	slices.Sort(ka)
	slices.Sort(kb)
	if len(ka) != 10 || !slices.Equal(ka, kb) {
		t.Errorf("keys differ:\n%v\n%v", ka, kb)
	}
}
