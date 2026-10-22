package keymgr

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pinRun is one import of a keyring at a time.
type pinRun struct {
	t     *testing.T
	store *PinStore
	mode  PinMode
}

func newPinRun(t *testing.T, mode PinMode) *pinRun {
	t.Helper()
	s, err := LoadPins(filepath.Join(t.TempDir(), "keypins.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &pinRun{t: t, store: s, mode: mode}
}

// at imports the keyring of keys at now and returns the keyids used, by
// name, why remailers are held, and the warnings.
func (pr *pinRun) at(now time.Time, keys ...testKey) (used map[string]string, held map[string]string, warnings []string) {
	pr.t.Helper()
	rems, err := readPubring(bytes.NewReader(testPubring(keys...)), now, false, func(string, ...any) {})
	if err != nil {
		pr.t.Fatal(err)
	}
	rems = dropAmbiguous(rems, func(string, ...any) {})
	use, held, events := pr.store.check(rems, pr.mode, now)
	used = make(map[string]string)
	for _, r := range use {
		if _, dup := used[r.name]; dup {
			pr.t.Fatalf("%s used twice", r.name)
		}
		used[r.name] = r.Address + " " + hex.EncodeToString(r.Keyid)
	}
	for _, e := range events {
		if e.Warn {
			warnings = append(warnings, e.Msg)
		}
	}
	return
}

func uses(k testKey) string { return k.addr + " " + k.keyid() }

var pinNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// Keys of remailer "alpha": the pinned one expires 2026-10-29, eight days
// after pinNow.
var (
	pinAlphaOld   = testKey{name: "alpha", addr: "alpha@example.org", seed: 10, from: "2026-10-15", to: "2026-10-29"}
	pinAlphaNew   = testKey{name: "alpha", addr: "alpha@example.org", seed: 11, from: "2026-10-21", to: "2026-11-04"}
	pinAlphaShort = testKey{name: "alpha", addr: "alpha@example.org", seed: 12, from: "2026-10-08", to: "2026-10-22"} // 36 h left at pinNow
	pinAlphaOlder = testKey{name: "alpha", addr: "alpha@example.org", seed: 13, from: "2026-10-14", to: "2026-10-28"}
	pinBeta       = testKey{name: "beta", addr: "beta@example.org", seed: 20, from: "2026-10-15", to: "2026-10-29"}
)

// TestPinFirstSeen checks that the first keyring is pinned and used.
func TestPinFirstSeen(t *testing.T) {
	pr := newPinRun(t, PinHold)
	used, held, warnings := pr.at(pinNow, pinAlphaOld, pinBeta)
	if used["alpha"] != uses(pinAlphaOld) || used["beta"] != uses(pinBeta) || len(held) > 0 || len(warnings) > 0 {
		t.Fatalf("used %v held %v warnings %q", used, held, warnings)
	}
	if p := pr.store.Pins["alpha@example.org"]; p == nil || p.Keyid != pinAlphaOld.keyid() || p.Expires != "2026-10-29" {
		t.Errorf("pin %+v", p)
	}
}

// TestPinRotation checks that a new key is accepted at once when the
// pinned one expires within 72 hours or already has, and that a new
// version or caps for the pinned key are accepted.
func TestPinRotation(t *testing.T) {
	pr := newPinRun(t, PinHold)
	pr.at(pinNow, pinAlphaShort)
	used, held, warnings := pr.at(pinNow.Add(time.Hour), pinAlphaNew)
	if used["alpha"] != uses(pinAlphaNew) || len(held) > 0 || len(warnings) > 0 {
		t.Fatalf("rotation: used %v held %v warnings %q", used, held, warnings)
	}
	// The pinned key has expired: anything newer is a rotation.
	pr = newPinRun(t, PinHold)
	pr.at(pinNow, pinAlphaOld)
	later := testKey{name: "alpha", addr: "alpha@example.org", seed: 14, from: "2026-11-10", to: "2026-11-24"}
	used, _, warnings = pr.at(time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC), later)
	if used["alpha"] != uses(later) || len(warnings) > 0 {
		t.Fatalf("after expiry: used %v warnings %q", used, warnings)
	}
	// Same key, new version and caps.
	pr = newPinRun(t, PinHold)
	pr.at(pinNow, pinAlphaOld)
	middle := pinAlphaOld
	middle.version, middle.caps = "4:0.2.7+gw1", "M"
	used, _, warnings = pr.at(pinNow.Add(time.Hour), middle)
	if used["alpha"] != uses(pinAlphaOld) || len(warnings) > 0 {
		t.Fatalf("new caps: used %v warnings %q", used, warnings)
	}
}

// TestPinHeld checks the changes that are held: a new key while the
// pinned one has more than 72 hours left, an older key, the address under
// another name, the name with another address.
func TestPinHeld(t *testing.T) {
	renamed := testKey{name: "gamma", addr: "alpha@example.org", seed: 10, from: "2026-10-15", to: "2026-10-29"}
	moved := testKey{name: "alpha", addr: "alpha@evil.example", seed: 15, from: "2026-10-21", to: "2026-11-04"}
	for _, c := range []struct {
		name string
		next testKey
		want string
	}{
		{"early key", pinAlphaNew, "valid for another 8 days"},
		{"older key", pinAlphaOlder, "before the pinned key"},
		{"renamed", renamed, "was pinned to the name alpha"},
		{"moved", moved, "was pinned to the address alpha@example.org"},
	} {
		pr := newPinRun(t, PinHold)
		pr.at(pinNow, pinAlphaOld, pinBeta)
		used, held, warnings := pr.at(pinNow.Add(time.Hour), c.next, pinBeta)
		if _, ok := used[c.next.name]; ok {
			t.Errorf("%s: used %v", c.name, used)
		}
		if used["beta"] != uses(pinBeta) {
			t.Errorf("%s: beta not used", c.name)
		}
		if !strings.Contains(held[c.next.name], c.want) || !strings.Contains(held[c.next.addr], c.want) {
			t.Errorf("%s: held %q, want %q", c.name, held, c.want)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "--pin-accept "+c.next.name) {
			t.Errorf("%s: warnings %q", c.name, warnings)
		}
	}
}

// TestPinHeldUsesPinnedKey checks that during a held key change the
// pinned key is used if the keyring still lists it.
func TestPinHeldUsesPinnedKey(t *testing.T) {
	pr := newPinRun(t, PinHold)
	pr.at(pinNow, pinAlphaOld)
	used, held, warnings := pr.at(pinNow.Add(time.Hour), pinAlphaOld, pinAlphaNew)
	if used["alpha"] != uses(pinAlphaOld) || len(held) > 0 || len(warnings) != 1 {
		t.Fatalf("used %v held %v warnings %q", used, held, warnings)
	}
}

// TestPinRelease checks that a held change is accepted after 24 hours and
// two sightings 30 minutes apart, and not while the old key is still
// published.
func TestPinRelease(t *testing.T) {
	h := func(d time.Duration) time.Time { return pinNow.Add(d) }

	pr := newPinRun(t, PinHold)
	pr.at(h(0), pinAlphaOld)
	pr.at(h(1*time.Hour), pinAlphaNew)
	if used, _, _ := pr.at(h(1*time.Hour+10*time.Minute), pinAlphaNew); used["alpha"] != "" {
		t.Fatal("used at once")
	}
	if pr.store.Held["alpha@example.org"].Sightings != 1 {
		t.Error("sightings 10 minutes apart counted")
	}
	if used, _, _ := pr.at(h(20*time.Hour), pinAlphaNew); used["alpha"] != "" {
		t.Fatal("used before 24 hours")
	}
	used, held, warnings := pr.at(h(25*time.Hour), pinAlphaNew)
	if used["alpha"] != uses(pinAlphaNew) || len(held) > 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "accepted after") {
		t.Fatalf("after 24 hours: used %v held %v warnings %q", used, held, warnings)
	}
	if p := pr.store.Pins["alpha@example.org"]; p.Keyid != pinAlphaNew.keyid() || len(pr.store.Held) > 0 {
		t.Errorf("pin %+v held %v", p, pr.store.Held)
	}

	// A client that runs rarely: the second sighting, a day later,
	// releases it.
	pr = newPinRun(t, PinHold)
	pr.at(h(0), pinAlphaOld)
	pr.at(h(1*time.Hour), pinAlphaNew)
	if used, _, _ := pr.at(h(26*time.Hour), pinAlphaNew); used["alpha"] != uses(pinAlphaNew) {
		t.Fatal("second sighting after 24 hours should release")
	}
	// Over 24 hours, but only one sighting: not released.
	pr = newPinRun(t, PinHold)
	pr.at(h(0), pinAlphaOld)
	pr.at(h(1*time.Hour), pinAlphaNew)
	pr.store.Held["alpha@example.org"].Since = h(-30 * time.Hour) // as if the first sighting was long ago
	pr.store.Held["alpha@example.org"].LastSighting = h(1 * time.Hour)
	if used, _, _ := pr.at(h(1*time.Hour+5*time.Minute), pinAlphaNew); used["alpha"] != "" {
		t.Fatal("released with one sighting")
	}

	// The old key comes back (the pingers flip between the two): not released.
	pr = newPinRun(t, PinHold)
	pr.at(h(0), pinAlphaOld)
	pr.at(h(1*time.Hour), pinAlphaNew)
	pr.at(h(10*time.Hour), pinAlphaOld)
	if used, held, _ := pr.at(h(30*time.Hour), pinAlphaNew); used["alpha"] != "" || held["alpha"] == "" {
		t.Fatalf("released although the old key came back: used %v", used)
	}
}

// TestPinWarn checks that with key_pinning warn a change is used at once
// with a warning.
func TestPinWarn(t *testing.T) {
	pr := newPinRun(t, PinWarn)
	pr.at(pinNow, pinAlphaOld)
	used, held, warnings := pr.at(pinNow.Add(time.Hour), pinAlphaNew)
	if used["alpha"] != uses(pinAlphaNew) || len(held) > 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "key_pinning: warn") {
		t.Fatalf("used %v held %v warnings %q", used, held, warnings)
	}
	if pr.store.Pins["alpha@example.org"].Keyid != pinAlphaNew.keyid() {
		t.Error("new key not pinned")
	}
}

// TestPinAcceptReject checks --pin-accept and --pin-reject.
func TestPinAcceptReject(t *testing.T) {
	moved := testKey{name: "alpha", addr: "alpha@new.example", seed: 15, from: "2026-10-21", to: "2026-11-04"}
	pr := newPinRun(t, PinHold)
	pr.at(pinNow, pinAlphaOld)
	pr.at(pinNow.Add(time.Hour), moved)
	if _, err := pr.store.Accept("nobody", pinNow); err == nil {
		t.Error("accepted a change that doesn't exist")
	}
	if _, err := pr.store.Accept("alpha", pinNow.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if pr.store.Pins["alpha@example.org"] != nil || pr.store.Pins["alpha@new.example"] == nil {
		t.Errorf("pins after accepting the move: %v", pr.store.Pins)
	}
	if used, _, warnings := pr.at(pinNow.Add(3*time.Hour), moved); used["alpha"] != uses(moved) || len(warnings) > 0 {
		t.Fatalf("after accept: used %v warnings %q", used, warnings)
	}

	pr = newPinRun(t, PinHold)
	pr.at(pinNow, pinAlphaOld)
	pr.at(pinNow.Add(time.Hour), pinAlphaNew)
	if _, err := pr.store.Reject("alpha@example.org"); err != nil {
		t.Fatal(err)
	}
	pr.at(pinNow.Add(2*time.Hour), pinAlphaNew)
	used, held, _ := pr.at(pinNow.Add(48*time.Hour), pinAlphaNew)
	if used["alpha"] != "" || !strings.Contains(held["alpha"], "rejected") {
		t.Fatalf("rejected change: used %v held %q", used, held)
	}
	// Forgotten after 30 days without being published; then it's a new
	// change again.
	pr.at(pinNow.Add(50*time.Hour), pinBeta)
	pr.at(pinNow.Add(33*24*time.Hour), pinBeta)
	if len(pr.store.Held) > 0 {
		t.Errorf("rejected change kept: %v", pr.store.Held)
	}
}

// TestPinRetention checks that pins of remailers no longer listed are
// dropped after 90 days, and held changes no longer published after 7.
func TestPinRetention(t *testing.T) {
	pinBeta := testKey{name: "beta", addr: "beta@example.org", seed: 20, from: "2026-10-15", to: "2027-12-31"}
	pr := newPinRun(t, PinHold)
	pr.at(pinNow, pinAlphaOld, pinBeta)
	pr.at(pinNow.Add(time.Hour), pinAlphaNew, pinBeta) // held
	pr.at(pinNow.Add(9*24*time.Hour), pinBeta)
	if len(pr.store.Held) > 0 {
		t.Errorf("held change kept after 8 days: %v", pr.store.Held)
	}
	if pr.store.Pins["alpha@example.org"] == nil {
		t.Error("pin dropped too early")
	}
	pr.at(pinNow.Add(91*24*time.Hour), pinBeta)
	if pr.store.Pins["alpha@example.org"] != nil || pr.store.Pins["beta@example.org"] == nil {
		t.Errorf("pins after 91 days: %v", pr.store.Pins)
	}
}

// TestPinStoreFile checks that the pins survive a save and load, and that
// a damaged file is an error rather than an empty store.
func TestPinStoreFile(t *testing.T) {
	pr := newPinRun(t, PinHold)
	pr.at(pinNow, pinAlphaOld)
	pr.at(pinNow.Add(time.Hour), pinAlphaNew)
	if err := pr.store.Save(); err != nil {
		t.Fatal(err)
	}
	s, err := LoadPins(pr.store.file)
	if err != nil {
		t.Fatal(err)
	}
	if s.Pins["alpha@example.org"].Keyid != pinAlphaOld.keyid() || s.Held["alpha@example.org"].Keyid != pinAlphaNew.keyid() {
		t.Errorf("loaded %+v", s)
	}
	for _, bad := range []string{"{", `{"pins":{"a@b":{"address":"c@d"}}}`, `{"held":{"a@b":null}}`} {
		os.WriteFile(pr.store.file, []byte(bad), 0600)
		if _, err := LoadPins(pr.store.file); err == nil {
			t.Errorf("%q loaded", bad)
		}
	}
}

// TestImportPubringPinning checks pinning through ImportPubring: a held
// remailer isn't a candidate, Get says why, and its stats don't warn.
func TestImportPubringPinning(t *testing.T) {
	now := time.Now().UTC()
	day := func(d int) string { return now.AddDate(0, 0, d).Format(date_format) }
	old := testKey{name: "alpha", addr: "alpha@example.org", seed: 30, from: day(-2), to: day(12)}
	early := testKey{name: "alpha", addr: "alpha@example.org", seed: 31, from: day(0), to: day(14)}
	beta := testKey{name: "beta", addr: "beta@example.org", seed: 32, from: day(-2), to: day(12)}

	dir := t.TempDir()
	pubFile, statsFile, pinFile := filepath.Join(dir, "pubring.mix"), filepath.Join(dir, "mlist2.txt"), filepath.Join(dir, "keypins.json")
	load := func(keys ...testKey) *Pubring {
		t.Helper()
		os.WriteFile(pubFile, testPubring(keys...), 0644)
		p := NewPubring(pubFile, statsFile)
		p.SetPinning(pinFile, PinHold)
		if err := p.ImportPubring(); err != nil {
			t.Fatal(err)
		}
		return p
	}
	load(old, beta)
	p := load(early, beta)
	if p.Count() != 1 {
		t.Errorf("%d keys, want only beta", p.Count())
	}
	for _, ref := range []string{"alpha", "alpha@example.org"} {
		if _, err := p.Get(ref); err == nil || !strings.Contains(err.Error(), "held by key pinning") {
			t.Errorf("Get(%s): %v", ref, err)
		}
	}
	if c := p.Candidates(0, 9999, 0, false); len(c) != 1 || c[0] != "beta@example.org" {
		t.Errorf("candidates %v", c)
	}
	if ev := p.PinEvents(); len(ev) != 1 || !ev[0].Warn {
		t.Errorf("events %+v", ev)
	}
	// A damaged pin file stops the import.
	os.WriteFile(pinFile, []byte("{"), 0600)
	q := NewPubring(pubFile, statsFile)
	q.SetPinning(pinFile, PinHold)
	if err := q.ImportPubring(); err == nil {
		t.Error("import with a damaged pin file succeeded")
	}
	// Without pinning nothing changes.
	q = NewPubring(pubFile, statsFile)
	if err := q.ImportPubring(); err != nil || q.Count() != 2 {
		t.Errorf("without pinning: %d keys, %v", q.Count(), err)
	}
}
