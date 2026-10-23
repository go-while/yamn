// Package minisigntest signs files in minisign's format, for tests of the
// verifier.  yamn itself never signs.
package minisigntest

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"time"

	"golang.org/x/crypto/blake2b"
)

// Key is a test signing key.
type Key struct {
	ID  [8]byte
	key ed25519.PrivateKey
}

// NewKey returns a key derived from seed, so tests are repeatable.
func NewKey(seed byte) *Key {
	k := &Key{key: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))}
	for i := range k.ID {
		k.ID[i] = seed + byte(i)
	}
	return k
}

// Public returns the public key's base64 line, as yamn's config takes it.
func (k *Key) Public() string {
	b := append([]byte("Ed"), k.ID[:]...)
	b = append(b, k.key.Public().(ed25519.PublicKey)...)
	return base64.StdEncoding.EncodeToString(b)
}

// Sign returns the signature file of data with the trusted comment
// trusted.
func (k *Key) Sign(data []byte, trusted string) []byte {
	h := blake2b.Sum512(data)
	sig := append([]byte("ED"), k.ID[:]...)
	sig = append(sig, ed25519.Sign(k.key, h[:])...)
	global := ed25519.Sign(k.key, append(bytes.Clone(sig[10:]), trusted...))
	return []byte("untrusted comment: test signature\n" +
		base64.StdEncoding.EncodeToString(sig) + "\n" +
		"trusted comment: " + trusted + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n")
}

// Comment returns a trusted comment for file, signed at ts and valid for
// valid.
func Comment(file string, ts time.Time, valid time.Duration) string {
	return fmt.Sprintf("timestamp:%d file:%s pinger:test expires:%d", ts.Unix(), file, ts.Add(valid).Unix())
}
