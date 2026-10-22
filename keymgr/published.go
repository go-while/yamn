package keymgr

import (
	"bytes"
	"encoding/hex"
	"strings"
	"time"
)

// Published is a key a keyring lists for a remailer.
type Published struct {
	Name    string
	Address string
	Keyid   string // hex
}

// PublishedFor returns the keys in pubring.mix content that use name or
// address (ignoring case), as a client would read them at now: valid, and
// with the keyid matching the public key.  A remailer uses it to check
// what pingers publish for it.
func PublishedFor(content []byte, name, address string, now time.Time) (keys []Published) {
	rems, _ := readPubring(bytes.NewReader(content), now, false, func(string, ...any) {})
	for _, r := range rems {
		if strings.EqualFold(r.name, name) || strings.EqualFold(r.Address, address) {
			keys = append(keys, Published{
				Name:    r.name,
				Address: r.Address,
				Keyid:   hex.EncodeToString(r.Keyid),
			})
		}
	}
	return
}

// Has reports whether keyid (hex) is in the secret keyring.
func (s *Secring) Has(keyid string) bool {
	_, ok := s.sec[keyid]
	return ok
}
