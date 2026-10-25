package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crooks/yamn/idlog"
)

// openTestIDLog opens an ID log for a test.
func openTestIDLog(t *testing.T, file string, days int) *idlog.IDLog {
	t.Helper()
	db, err := idlog.NewIDLog(file, days)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// openTestChunk opens a chunk database for a test.
func openTestChunk(t *testing.T, file string) *Chunk {
	t.Helper()
	db, err := OpenChunk(file)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// TestDBLocked checks that a database open in another process (here: a
// second open) gives an error, not a panic, and that the message points
// at a second remailer.  A second remailer on the same files used to
// panic with "resource temporarily unavailable".
func TestDBLocked(t *testing.T) {
	dir := t.TempDir()
	idFile, chunkFile := filepath.Join(dir, "idlog"), filepath.Join(dir, "chunkdb")
	first := openTestIDLog(t, idFile, 14)
	defer first.Close()
	if _, err := idlog.NewIDLog(idFile, 14); err == nil {
		t.Error("a second open of the ID log succeeded")
	} else if msg := dbOpenError("ID log", idFile, err); !strings.Contains(msg, "another yamn remailer") {
		t.Errorf("message %q doesn't point at a second remailer", msg)
	}
	chunk := openTestChunk(t, chunkFile)
	defer chunk.Close()
	if _, err := OpenChunk(chunkFile); err == nil {
		t.Error("a second open of the chunk database succeeded")
	} else if msg := dbOpenError("chunk database", chunkFile, err); !strings.Contains(msg, chunkFile) {
		t.Errorf("message %q doesn't name the file", msg)
	}
	// Another error is reported as it is.
	if msg := dbOpenError("ID log", "x", errTest); strings.Contains(msg, "another") || !strings.Contains(msg, "test error") {
		t.Errorf("other error: %q", msg)
	}
}

type testError struct{}

func (testError) Error() string { return "test error" }

var errTest = testError{}
