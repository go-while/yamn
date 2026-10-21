package keymgr

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

const htmlErrorPage = "<!DOCTYPE html>\n<html><body><h1>Service Unavailable</h1></body></html>\n"

func TestCheckPubring(t *testing.T) {
	pubring := fakePubring()
	if err := CheckPubring([]byte(pubring)); err != nil {
		t.Errorf("Valid pubring rejected: %s", err)
	}
	// Cut the file off in the middle of the first key block.
	truncated := pubring[:strings.Index(pubring, "-----End Mix Key-----")]
	for name, b := range map[string]string{
		"empty":     "",
		"HTML page": htmlErrorPage,
		"truncated": truncated,
		"no header": "-----Begin Mix Key-----\nabc\ndef\n-----End Mix Key-----\n",
	} {
		if err := CheckPubring([]byte(b)); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
}

// TestCheckLikeParser checks that downloads the parsers can't use, or that
// hold no usable key or stats, are rejected.
func TestCheckLikeParser(t *testing.T) {
	now := time.Now().UTC()
	valid := testKey{name: "alpha", addr: "alpha@example.org", seed: 1,
		from: now.AddDate(0, 0, -1).Format(date_format), to: now.AddDate(0, 0, 13).Format(date_format)}
	expired := testKey{name: "beta", addr: "beta@example.org", seed: 2,
		from: now.AddDate(0, 0, -30).Format(date_format), to: now.AddDate(0, 0, -2).Format(date_format)}
	ring := testPubring(valid)
	if err := CheckPubring(ring); err != nil {
		t.Fatalf("valid pubring rejected: %s", err)
	}
	for name, b := range map[string][]byte{
		// A complete block followed by a line the scanner can't read.
		"line over 64 KB": append(testPubring(valid), []byte(strings.Repeat("x", 70000)+"\n")...),
		// The only key fails the keyid check.
		"keyid mismatch": bytes.Replace(ring, []byte(hex.EncodeToString(valid.pk())),
			[]byte(hex.EncodeToString(expired.pk())), 1),
		"only expired keys": testPubring(expired),
	} {
		if err := CheckPubring(b); err == nil {
			t.Errorf("pubring with %s accepted", name)
		}
	}
	// The merge uses its own time and use_expired.
	if err := checkPubring(testPubring(expired), now, true); err != nil {
		t.Errorf("use_expired: %s", err)
	}

	full := testStats(time.Hour, "alpha 10 100", "beta 10 100")
	if err := CheckStats(full); err != nil {
		t.Fatalf("valid stats rejected: %s", err)
	}
	for name, b := range map[string][]byte{
		// Cut in the middle of the table (no blank line after it).
		"truncated table": full[:bytes.Index(full, []byte("beta"))],
		"no rows":         testStats(time.Hour),
	} {
		if err := CheckStats(b); err == nil {
			t.Errorf("stats with %s accepted", name)
		}
	}
}

func TestCheckStats(t *testing.T) {
	if err := CheckStats([]byte(fakeMlist2())); err != nil {
		t.Errorf("Valid stats rejected: %s", err)
	}
	for name, b := range map[string]string{
		"empty":         "",
		"HTML page":     htmlErrorPage,
		"bad timestamp": "Generated: yesterday\n" + strings.Repeat("-", 64) + "\n",
		"no table":      "Generated: Mon 02 Jan 2006 15:04:05 GMT\n",
	} {
		if err := CheckStats([]byte(b)); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
}
