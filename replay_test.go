package main

import (
	"bytes"
	"testing"
)

func TestReplayLogDays(t *testing.T) {
	setupTestConfig(t)
	for _, tt := range []struct{ idExpire, maxAge, want int }{
		{16, 14, 16}, // the default: kept
		{15, 14, 15}, // exactly max_age+1: kept
		{30, 14, 30}, // longer: kept
		{14, 14, 16}, // equal to max_age: raised
		{3, 14, 16},  // much shorter: raised
		{5, 30, 32},
	} {
		cfg.Remailer.IDexp, cfg.Remailer.MaxAge = tt.idExpire, tt.maxAge
		if got := replayLogDays(); got != tt.want {
			t.Errorf("id_expire=%d, max_age=%d: got %d days, want %d", tt.idExpire, tt.maxAge, got, tt.want)
		}
	}
}

// TestDefaultReplayLogDays checks that the default config doesn't need
// clamping.
func TestDefaultReplayLogDays(t *testing.T) {
	setupTestConfig(t)
	if cfg.Remailer.IDexp < cfg.Remailer.MaxAge+1 {
		t.Errorf("Default id_expire (%d) is shorter than max_age+1 (%d)", cfg.Remailer.IDexp, cfg.Remailer.MaxAge+1)
	}
}

// TestTamperedCopyDoesNotBurnID checks that a tampered copy of a packet,
// arriving before the genuine one, doesn't get the genuine packet rejected
// as a replay.
func TestTamperedCopyDoesNotBurnID(t *testing.T) {
	_, exit := setupRoundTrip(t)
	packet := encodeTestMsg([]byte("Hello"), exit)
	tampered := bytes.Clone(packet)
	tampered[len(tampered)-1] ^= 0x01
	pool := actAs(t, exit, true)

	decodeMsg(tampered, exit.secring)
	if n := len(poolFiles(t, pool, "")); n != 0 {
		t.Fatalf("Tampered copy should not be delivered, got %d files", n)
	}
	if err := decodeMsg(packet, exit.secring); err != nil {
		t.Fatalf("Genuine packet rejected after a tampered copy: %s", err)
	}
	if n := len(poolFiles(t, pool, "m")); n != 1 {
		t.Fatalf("Genuine packet should be delivered, got %d files", n)
	}
}
