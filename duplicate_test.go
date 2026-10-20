package main

import (
	"bytes"
	stdlog "log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/log-go"
)

// captureLog sends log output at Info level and above to a buffer for the
// rest of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := new(bytes.Buffer)
	oldCurrent, oldWriter := log.Current, stdlog.Writer()
	stdlog.SetOutput(buf)
	log.Current = log.StdLogger{Level: log.InfoLevel}
	t.Cleanup(func() {
		log.Current = oldCurrent
		stdlog.SetOutput(oldWriter)
	})
	return buf
}

// TestDuplicateCopyNotLogged puts two copies of a packet into the exit's
// inbound pool: the first is delivered, the second must not produce a
// warning or info line.
func TestDuplicateCopyNotLogged(t *testing.T) {
	_, exit := setupRoundTrip(t)
	pool := actAs(t, exit, true)
	packet := encodeTestMsg([]byte("Hello"), exit)
	for _, name := range []string{"icopy1", "icopy2"} {
		if err := os.WriteFile(filepath.Join(pool, name), packet, 0600); err != nil {
			t.Fatal(err)
		}
	}
	logged := captureLog(t)
	processInpool("i", exit.secring)
	if n := len(poolFiles(t, pool, "m")); n != 1 {
		t.Fatalf("Expected 1 delivered message, got %d", n)
	}
	if strings.Contains(logged.String(), "collision") || strings.Contains(logged.String(), "duplicate") {
		t.Errorf("A duplicate copy should only be logged at Trace level, got:\n%s", logged)
	}
}

// TestOtherDecodeErrorsLogged checks that real decoding problems are still
// logged.
func TestOtherDecodeErrorsLogged(t *testing.T) {
	middle, exit := setupRoundTrip(t)
	pool := actAs(t, middle, true)
	// A packet for the exit, arriving at the middle: its key is unknown here.
	if err := os.WriteFile(filepath.Join(pool, "iother"), encodeTestMsg([]byte("Hello"), exit), 0600); err != nil {
		t.Fatal(err)
	}
	logged := captureLog(t)
	processInpool("i", middle.secring)
	if !strings.Contains(logged.String(), "Keyid not found") {
		t.Errorf("Unknown recipient key should still be logged, got:\n%s", logged)
	}
}
