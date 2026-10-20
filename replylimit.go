package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"sync"
	"time"

	"github.com/crooks/yamn/crandom"
)

// replyLimiter allows one remailer-* reply per address per day (UTC).  The
// From address of a request can be forged, so without a limit anyone could
// make a remailer mail its key or help text to a third party any number of
// times.
//
// Addresses are not stored: only the first 8 bytes of an HMAC-SHA256 of each
// address, keyed with a random secret generated at startup.  That's about 8
// bytes per address instead of the whole string, accidental collisions are
// negligible at 64 bits, and without the key nobody can craft an address
// whose hash collides with someone else's to block their replies.
type replyLimiter struct {
	mu   sync.Mutex
	key  []byte
	day  int64 // UTC day number the set below belongs to
	sent map[uint64]struct{}
	now  func() time.Time // replaced in tests
}

func newReplyLimiter() *replyLimiter {
	return &replyLimiter{
		key:  crandom.Randbytes(32),
		sent: make(map[uint64]struct{}),
		now:  time.Now,
	}
}

// hash returns the keyed 64-bit hash of an address, ignoring case.
func (l *replyLimiter) hash(addr string) uint64 {
	mac := hmac.New(sha256.New, l.key)
	mac.Write([]byte(strings.ToLower(addr)))
	return binary.BigEndian.Uint64(mac.Sum(nil))
}

// rollover clears the set when a new UTC day has started.  The caller holds
// l.mu.
func (l *replyLimiter) rollover() {
	if day := l.now().UTC().Unix() / 86400; day != l.day {
		l.day = day
		l.sent = make(map[uint64]struct{})
	}
}

// allowed reports whether addr hasn't had a reply yet today.
func (l *replyLimiter) allowed(addr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rollover()
	_, done := l.sent[l.hash(addr)]
	return !done
}

// record notes that addr got a reply today.
func (l *replyLimiter) record(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rollover()
	l.sent[l.hash(addr)] = struct{}{}
}

// fooReplies limits replies to remailer-* requests.
var fooReplies = newReplyLimiter()
