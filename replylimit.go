package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
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
//
// With a file (load), the key, the day and the hashes are kept there, so the
// limit also holds across processes, such as remailer runs from cron.
type replyLimiter struct {
	mu   sync.Mutex
	key  []byte
	day  int64 // UTC day number the set below belongs to
	sent map[uint64]struct{}
	now  func() time.Time // replaced in tests
	file string           // where the state is kept, if anywhere
}

// replyLimitMagic starts the limiter's file: "YRL1", then the 32-byte key,
// the day (int64) and the 8-byte hashes, all big-endian.
var replyLimitMagic = []byte("YRL1")

// load reads the limiter's state from file and keeps it there from now on.
// A missing file starts a fresh limiter; a damaged one is replaced.
func (l *replyLimiter) load(file string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.file = file
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(b) < len(replyLimitMagic)+32+8 || !bytes.HasPrefix(b, replyLimitMagic) ||
		(len(b)-len(replyLimitMagic)-40)%8 != 0 {
		return fmt.Errorf("%s is damaged, starting a new one", file)
	}
	b = b[len(replyLimitMagic):]
	l.key = append([]byte(nil), b[:32]...)
	l.day = int64(binary.BigEndian.Uint64(b[32:40]))
	l.sent = make(map[uint64]struct{})
	for b = b[40:]; len(b) > 0; b = b[8:] {
		l.sent[binary.BigEndian.Uint64(b[:8])] = struct{}{}
	}
	return nil
}

// save writes the state to the file, if there is one.  The caller holds
// l.mu.
func (l *replyLimiter) save() error {
	if l.file == "" {
		return nil
	}
	b := append([]byte(nil), replyLimitMagic...)
	b = append(b, l.key...)
	b = binary.BigEndian.AppendUint64(b, uint64(l.day))
	for h := range l.sent {
		b = binary.BigEndian.AppendUint64(b, h)
	}
	return writeFileAtomic(l.file, b, 0600)
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

// record notes that addr got a reply today.  An error means the note
// couldn't be saved: the limit then holds for this process only.
func (l *replyLimiter) record(addr string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rollover()
	l.sent[l.hash(addr)] = struct{}{}
	return l.save()
}

// fooReplies limits replies to remailer-* requests.
var fooReplies = newReplyLimiter()
