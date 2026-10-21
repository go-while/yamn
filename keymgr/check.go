package keymgr

import (
	"bytes"
	"errors"
	"time"
)

// CheckPubring checks that b is a pubring.mix file with at least one key
// that is valid now, read by the same parser as ImportPubring.  It's used to
// reject a download (an HTML error page, an empty, truncated or outdated
// file) before it replaces a working keyring.
func CheckPubring(b []byte) error {
	return checkPubring(b, time.Now(), false)
}

// checkPubring is CheckPubring at the time now; useExpired accepts expired
// keys, as for ImportPubring.
func checkPubring(b []byte, now time.Time, useExpired bool) error {
	rems, err := readPubring(bytes.NewReader(b), now, useExpired, func(string, ...any) {})
	if err != nil {
		return err
	}
	if len(rems) == 0 {
		return errors.New("no valid key found")
	}
	return nil
}

// CheckStats checks that b is an mlist2.txt stats file with at least one
// remailer, read by the same parser as ImportStats.
func CheckStats(b []byte) error {
	_, rows, err := readStats(bytes.NewReader(b), "stats", func(string, ...any) {})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("no stats found")
	}
	return nil
}
