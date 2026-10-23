package minisign_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crooks/yamn/minisign"
	"github.com/crooks/yamn/minisign/minisigntest"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestVectors verifies go-echolot's and the minisign tool's signatures of
// the shared test vectors.
func TestVectors(t *testing.T) {
	pub, err := minisign.ParsePublicKey(string(read(t, "pinger.pub")))
	if err != nil {
		t.Fatal(err)
	}
	if pub.ID.String() != "89F235255BD21020" {
		t.Errorf("key ID %s", pub.ID)
	}
	// The base64 line alone is the same key.
	line := strings.Split(string(read(t, "pinger.pub")), "\n")[1]
	if p2, err := minisign.ParsePublicKey(line); err != nil || p2.ID != pub.ID || !bytes.Equal(p2.Key, pub.Key) {
		t.Errorf("base64 line: %v", err)
	}
	for _, c := range []struct {
		file    string
		expires time.Duration
	}{{"pubring.mix", 48 * time.Hour}, {"mlist2.txt", 6 * time.Hour}} {
		data := read(t, c.file)
		for _, sigFile := range []string{c.file + ".minisig", c.file + ".minisign-tool.minisig"} {
			sig, err := minisign.ParseSignature(read(t, sigFile))
			if err != nil {
				t.Fatalf("%s: %v", sigFile, err)
			}
			if _, err := sig.Verify([]minisign.PublicKey{pub}, data); err != nil {
				t.Errorf("%s: %v", sigFile, err)
			}
			tr, err := minisign.ParseTrusted(sig.Trusted)
			if err != nil {
				t.Fatalf("%s: %v", sigFile, err)
			}
			want := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
			if tr.File != c.file || !tr.Timestamp.Equal(want) || !tr.Expires.Equal(want.Add(c.expires)) {
				t.Errorf("%s: trusted comment %+v", sigFile, tr)
			}
			// One changed byte in the file.
			bad := bytes.Clone(data)
			bad[len(bad)/2] ^= 1
			if _, err := sig.Verify([]minisign.PublicKey{pub}, bad); !errors.Is(err, minisign.ErrBadSignature) {
				t.Errorf("%s: changed file: %v", sigFile, err)
			}
		}
	}
}

// TestVerify checks the signature checks with a test key.
func TestVerify(t *testing.T) {
	k, other := minisigntest.NewKey(1), minisigntest.NewKey(2)
	pub, _ := minisign.ParsePublicKey(k.Public())
	otherPub, _ := minisign.ParsePublicKey(other.Public())
	data := []byte("some keyring\n")
	trusted := minisigntest.Comment("pubring.mix", time.Unix(1792670400, 0), time.Hour)
	good := k.Sign(data, trusted)

	sig, err := minisign.ParseSignature(good)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := sig.Verify([]minisign.PublicKey{otherPub, pub}, data); err != nil || got.ID != pub.ID {
		t.Errorf("second key of two: %v", err)
	}
	if _, err := sig.Verify([]minisign.PublicKey{otherPub}, data); !errors.Is(err, minisign.ErrUnknownKey) {
		t.Errorf("other key: %v", err)
	}
	// Same key ID, another key: not accepted.
	impostor := minisign.PublicKey{ID: pub.ID, Key: otherPub.Key}
	if _, err := sig.Verify([]minisign.PublicKey{impostor}, data); !errors.Is(err, minisign.ErrBadSignature) {
		t.Errorf("impostor key: %v", err)
	}
	// A trusted comment changed after signing.
	changed := bytes.Replace(good, []byte("expires:"), []byte("expires:9"), 1)
	if s, err := minisign.ParseSignature(changed); err != nil {
		t.Fatal(err)
	} else if _, err := s.Verify([]minisign.PublicKey{pub}, data); !errors.Is(err, minisign.ErrBadSignature) {
		t.Errorf("changed trusted comment: %v", err)
	}
	// CR LF and blank lines after the four are accepted.
	crlf := bytes.ReplaceAll(good, []byte("\n"), []byte("\r\n"))
	if s, err := minisign.ParseSignature(append(crlf, "\r\n\n"...)); err != nil {
		t.Errorf("CR LF: %v", err)
	} else if _, err := s.Verify([]minisign.PublicKey{pub}, data); err != nil {
		t.Errorf("CR LF: %v", err)
	}
}

func TestParseSignatureErrors(t *testing.T) {
	k := minisigntest.NewKey(1)
	good := string(k.Sign([]byte("x"), "timestamp:1 file:a expires:2"))
	lines := strings.Split(good, "\n")
	legacy := strings.Replace(good, lines[1], "RWQ"+lines[1][3:], 1) // "Ed" instead of "ED"
	for name, s := range map[string]string{
		"empty":           "",
		"three lines":     strings.Join(lines[:3], "\n"),
		"five lines":      good + "extra\n",
		"no untrusted":    strings.Replace(good, "untrusted comment: ", "", 1),
		"no trusted":      strings.Replace(good, "trusted comment: timestamp", "timestamp", 1),
		"bad base64":      strings.Replace(good, lines[1], lines[1][1:], 1),
		"legacy":          legacy,
		"bad global":      strings.Replace(good, lines[3], lines[3][:20], 1),
		"too long":        good + strings.Repeat("\n", minisign.MaxSignatureFile),
		"untrusted later": strings.Replace(good, "untrusted comment: ", "Untrusted comment: ", 1),
	} {
		if _, err := minisign.ParseSignature([]byte(s)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseTrusted(t *testing.T) {
	tr, err := minisign.ParseTrusted("expires:200 future_key:x:y file:pubring.mix timestamp:100")
	if err != nil {
		t.Fatal(err)
	}
	if tr.File != "pubring.mix" || tr.Timestamp.Unix() != 100 || tr.Expires.Unix() != 200 {
		t.Errorf("%+v", tr)
	}
	for _, s := range []string{
		"",
		"timestamp:100 file:pubring.mix", // no expires
		"timestamp:100 expires:200",      // no file
		"timestamp:100 file:a expires:200 file:b",                          // twice
		"timestamp:100  file:a expires:200",                                // two spaces
		" timestamp:100 file:a expires:200",                                // leading space
		"timestamp:100 file:a expires:200 ",                                // trailing space
		"timestamp:-100 file:a expires:200",                                // sign
		"timestamp:+100 file:a expires:200",                                // sign
		"timestamp:1e3 file:a expires:20000",                               // not decimal
		"timestamp:1234567890123456789 file:a expires:1234567890123456790", // 19 digits
		"timestamp:200 file:a expires:200",                                 // not after
		"timestamp:100 file:a expires:200 Pinger:x",                        // upper case key
		"timestamp:100 file:a expires:200 pinger:",                         // empty value
		"timestamp:100 file:a\texpires:200",                                // tab
		"timestamp:100 file:é expires:200",                                 // not ASCII
	} {
		if _, err := minisign.ParseTrusted(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestParsePublicKeyErrors(t *testing.T) {
	good := minisigntest.NewKey(1).Public()
	for name, s := range map[string]string{
		"empty":       "",
		"short":       good[:20],
		"signature":   "RUQ" + good[3:], // "ED" tag
		"no comment":  "minisign public key\n" + good,
		"not base64":  good[:len(good)-2] + "!!",
		"extra lines": "untrusted comment: x\n" + good + "\n" + good,
	} {
		if _, err := minisign.ParsePublicKey(s); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
