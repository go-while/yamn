// Package minisign verifies the signatures pingers publish next to their
// files (<file>.minisig), in the format of the minisign tool
// (https://jedisct1.github.io/minisign/), and reads their trusted comment.
//
// Only minisign's default signatures are accepted: Ed25519 over the
// BLAKE2b-512 hash of the file ("ED").  The trusted comment is the
// contract with go-echolot (its docs/design.md, section 9.5):
//
//	trusted = pair *( SP pair )          ; one space between pairs
//	pair    = key ":" value              ; any order, each key at most once
//	key     = 1*( %x61-7A / DIGIT / "_" ) ; a-z, 0-9, _
//	value   = 1*( %x21-7E )              ; printable ASCII, no space
//
// timestamp and expires (Unix seconds UTC, 1 to 18 digits) and file (the
// published file's base name) are required.  Other keys, such as pinger,
// are ignored.
package minisign

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/blake2b"
)

const (
	keyIDLen     = 8
	publicKeyLen = 2 + keyIDLen + ed25519.PublicKeySize // "Ed", key ID, key
	sigLen       = 2 + keyIDLen + ed25519.SignatureSize // "ED", key ID, signature
	// MaxSignatureFile is the longest signature file read: four lines
	// with comments of up to minisign's 8 KiB.
	MaxSignatureFile = 16 << 10

	untrustedPrefix = "untrusted comment: "
	trustedPrefix   = "trusted comment: "
)

// KeyID is the number minisign gives a key pair; signatures carry it.
type KeyID [keyIDLen]byte

// String is the key ID as minisign prints it: hex of the little-endian
// number.
func (id KeyID) String() string {
	return fmt.Sprintf("%016X", binary.LittleEndian.Uint64(id[:]))
}

// PublicKey is a minisign public key.
type PublicKey struct {
	ID  KeyID
	Key ed25519.PublicKey
}

// ParsePublicKey reads a public key: its base64 line (what `minisign -P`
// takes), or minisign's two-line public key file.
func ParsePublicKey(s string) (PublicKey, error) {
	s = strings.TrimSpace(s)
	if first, rest, ok := strings.Cut(s, "\n"); ok {
		if !strings.HasPrefix(first, untrustedPrefix) {
			return PublicKey{}, errors.New("not a minisign public key")
		}
		s = strings.TrimSpace(rest)
	}
	b, err := base64.StdEncoding.Strict().DecodeString(s)
	if err != nil || len(b) != publicKeyLen || string(b[:2]) != "Ed" {
		return PublicKey{}, errors.New("not a minisign public key")
	}
	var p PublicKey
	copy(p.ID[:], b[2:2+keyIDLen])
	p.Key = ed25519.PublicKey(bytes.Clone(b[2+keyIDLen:]))
	return p, nil
}

// Signature is a parsed signature file.
type Signature struct {
	KeyID   KeyID
	sig     []byte // over BLAKE2b-512 of the file
	Trusted string // the trusted comment, not yet verified
	global  []byte // over sig and Trusted
}

// ParseSignature reads a signature file: four lines, as minisign writes
// them.  Lines may end in CR LF, and blank lines after the four are
// ignored, as minisign does.
func ParseSignature(data []byte) (*Signature, error) {
	if len(data) > MaxSignatureFile {
		return nil, errors.New("signature file too long")
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if len(lines) != 4 {
		return nil, errors.New("a signature file has four lines")
	}
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	if !strings.HasPrefix(lines[0], untrustedPrefix) {
		return nil, errors.New("signature file: no untrusted comment")
	}
	var s Signature
	var ok bool
	if s.Trusted, ok = strings.CutPrefix(lines[2], trustedPrefix); !ok {
		return nil, errors.New("signature file: no trusted comment")
	}
	sig, err := base64.StdEncoding.Strict().DecodeString(lines[1])
	if err != nil || len(sig) != sigLen {
		return nil, errors.New("signature file: bad signature line")
	}
	switch string(sig[:2]) {
	case "ED":
	case "Ed":
		return nil, errors.New("signature file: legacy signature (minisign -l), not accepted")
	default:
		return nil, fmt.Errorf("signature file: unknown algorithm %q", sig[:2])
	}
	copy(s.KeyID[:], sig[2:2+keyIDLen])
	s.sig = sig[2+keyIDLen:]
	if s.global, err = base64.StdEncoding.Strict().DecodeString(lines[3]); err != nil || len(s.global) != ed25519.SignatureSize {
		return nil, errors.New("signature file: bad trusted comment signature")
	}
	return &s, nil
}

// Errors of Verify.
var (
	ErrUnknownKey   = errors.New("signed by an unknown key")
	ErrBadSignature = errors.New("bad signature")
)

// Verify checks that one of keys signed data and the trusted comment, and
// returns the key that did.
func (s *Signature) Verify(keys []PublicKey, data []byte) (PublicKey, error) {
	for _, k := range keys {
		if k.ID != s.KeyID {
			continue
		}
		h := blake2b.Sum512(data)
		if !ed25519.Verify(k.Key, h[:], s.sig) ||
			!ed25519.Verify(k.Key, append(bytes.Clone(s.sig), s.Trusted...), s.global) {
			return PublicKey{}, ErrBadSignature
		}
		return k, nil
	}
	return PublicKey{}, fmt.Errorf("%w %s", ErrUnknownKey, s.KeyID)
}

// Trusted holds the trusted comment's values.
type Trusted struct {
	Timestamp time.Time // when it was signed
	Expires   time.Time
	File      string // the signed file's base name
}

// ParseTrusted reads a trusted comment by the grammar in the package
// comment.
func ParseTrusted(s string) (Trusted, error) {
	var t Trusted
	seen := make(map[string]bool)
	for _, pair := range strings.Split(s, " ") {
		k, v, ok := strings.Cut(pair, ":")
		if !ok || !validKey(k) || !validValue(v) {
			return Trusted{}, fmt.Errorf("trusted comment: bad pair %q", pair)
		}
		if seen[k] {
			return Trusted{}, fmt.Errorf("trusted comment: %s twice", k)
		}
		seen[k] = true
		switch k {
		case "timestamp", "expires":
			u, ok := unixSeconds(v)
			if !ok {
				return Trusted{}, fmt.Errorf("trusted comment: %s %q is not Unix seconds", k, v)
			}
			if k == "timestamp" {
				t.Timestamp = u
			} else {
				t.Expires = u
			}
		case "file":
			t.File = v
		}
	}
	for _, k := range []string{"timestamp", "expires", "file"} {
		if !seen[k] {
			return Trusted{}, fmt.Errorf("trusted comment: no %s", k)
		}
	}
	if !t.Expires.After(t.Timestamp) {
		return Trusted{}, errors.New("trusted comment: expires isn't after timestamp")
	}
	return t, nil
}

func validKey(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return s != ""
}

func validValue(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return s != ""
}

// unixSeconds parses 1 to 18 decimal digits as Unix seconds.
func unixSeconds(s string) (time.Time, bool) {
	if s == "" || len(s) > 18 {
		return time.Time{}, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return time.Time{}, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return time.Unix(n, 0).UTC(), err == nil
}
