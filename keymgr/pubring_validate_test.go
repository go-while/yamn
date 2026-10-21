package keymgr

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReadPubringValidatesHeader checks that keys with a name or address
// yamn can't use are skipped, and the others kept.
func TestReadPubringValidatesHeader(t *testing.T) {
	long := strings.Repeat("x", 52-len("@example.org")) + "@example.org" // 52 bytes
	for _, c := range []struct {
		name, addr string
		ok         bool
	}{
		{"alpha", "alpha@example.org", true},
		{"yamn2", "yamn2@mixmin.net", true},
		{"alpha", long, true},
		{"alpha", "x" + long, false}, // 53 bytes: over the next-hop field
		{"alpha", "alpha.example.org", false},
		{"alpha", "@example.org", false},
		{"alpha", "alpha@", false},
		{"alpha", "a@b@example.org", false},
		{"alpha", "alpha@exa\x01mple.org", false},
		{"alpha", "alpha@exa\tmple.org", false},
		{"alpha", "alpha,victim@example.org", false},
		{"alpha", "<alpha@example.org>", false},
		{"alpha", "alpha@exämple.org", false},
		{"Alpha", "alpha@example.org", false},
		{"al@pha", "alpha@example.org", false},
		{"al%pha", "alpha@example.org", false},
		{"al-pha", "alpha@example.org", false},
		{"*", "alpha@example.org", false},
	} {
		k := testKey{name: c.name, addr: c.addr, seed: 1}
		var warned bool
		rems, err := readPubring(bytes.NewReader(testPubring(k)), mergeNow, false, func(string, ...any) { warned = true })
		if err != nil {
			t.Fatal(err)
		}
		if got := len(rems) == 1; got != c.ok {
			t.Errorf("name %q address %q: accepted=%v, want %v", c.name, c.addr, got, c.ok)
		}
		if warned == c.ok {
			t.Errorf("name %q address %q: warned=%v", c.name, c.addr, warned)
		}
	}
}

// TestImportPubringAmbiguous checks that a name used by two addresses, or
// an address used by two names, is ignored on import, while a key rotation
// (two keys of one remailer) is kept.
func TestImportPubringAmbiguous(t *testing.T) {
	impostor := testKey{name: "alpha", addr: "impostor@example.org", seed: 3}
	alias := testKey{name: "alias", addr: "beta@example.org", seed: 4}
	rotated := testKey{name: "gamma", addr: "gamma@example.org", seed: 5, from: "2026-10-20", to: "2026-11-03"}
	file := filepath.Join(t.TempDir(), "pubring.mix")
	os.WriteFile(file, testPubring(alpha, impostor, beta, alias, gamma, rotated), 0600)
	p := NewPubring(file, "")
	p.useExpired = true // the test keys' dates are fixed
	if err := p.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"alpha", "alpha@example.org", "impostor@example.org", "beta", "alias", "beta@example.org"} {
		if r, err := p.Get(ref); err == nil {
			t.Errorf("Get(%s) returned %s %s", ref, r.name, r.Address)
		}
	}
	if r, err := p.Get("gamma"); err != nil || !bytes.Equal(r.PK, rotated.pk()) {
		t.Errorf("rotated key: %v %v", r.PK, err)
	}
	if p.Count() != 1 {
		t.Errorf("Count=%d, want 1", p.Count())
	}
}

// TestRealPubringsValid checks that the validation keeps every key of real
// pubrings published by Echolot pingers.
func TestRealPubringsValid(t *testing.T) {
	files, _ := filepath.Glob("testdata/pubring-*.mix")
	if len(files) == 0 {
		t.Fatal("no test pubrings")
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rems, err := readPubring(bytes.NewReader(b), now, true, func(format string, a ...any) {
			t.Errorf("%s: "+format, append([]any{f}, a...)...)
		})
		if err != nil {
			t.Fatal(err)
		}
		want := bytes.Count(b, []byte("-----Begin Mix Key-----"))
		if len(rems) != want || len(dropAmbiguous(rems, t.Errorf)) != want {
			t.Errorf("%s: %d of %d keys read", f, len(rems), want)
		}
	}
}

// TestSetNameAddressValidated checks that a remailer can't be configured
// with a name or address that clients ignore.
func TestSetNameAddressValidated(t *testing.T) {
	mustPanic := func(what string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s accepted", what)
			}
		}()
		f()
	}
	s := NewSecring("", "")
	s.SetName("Yamn2")
	s.SetAddress("Yamn2@Example.org")
	if s.name != "yamn2" || s.address != "yamn2@example.org" {
		t.Errorf("name %q address %q", s.name, s.address)
	}
	mustPanic("name my-rem", func() { s.SetName("my-rem") })
	mustPanic("address a<b@example.org", func() { s.SetAddress("a<b@example.org") })
	mustPanic("address a b@example.org", func() { s.SetAddress("a b@example.org") })
}
