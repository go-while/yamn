package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSlotFinalValidate(t *testing.T) {
	tests := []struct {
		name      string
		bodyBytes int
		chunkNum  uint8
		numChunks uint8
		wantErr   bool
	}{
		{"empty body", 0, 1, 1, false},
		{"maximum body", bodyBytes, 1, 1, false},
		{"last of several chunks", 100, 3, 3, false},
		{"body too long", bodyBytes + 1, 1, 1, true},
		{"negative body length", -1, 1, 1, true},
		{"zero chunks", 100, 1, 0, true},
		{"chunk number zero", 100, 0, 2, true},
		{"chunk number above count", 100, 3, 2, true},
	}
	for _, tt := range tests {
		f := &slotFinal{bodyBytes: tt.bodyBytes, chunkNum: tt.chunkNum, numChunks: tt.numChunks}
		err := f.validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: validate() error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}

// setupChunkDb gives the test an exit remailer with an empty pool and chunk DB.
func setupChunkDb(t *testing.T) string {
	t.Helper()
	_, exit := setupRoundTrip(t)
	pool := actAs(t, exit, true)
	oldChunkDb := ChunkDb
	ChunkDb = OpenChunk(filepath.Join(t.TempDir(), "chunkdb"))
	ChunkDb.SetExpire(60)
	t.Cleanup(func() {
		ChunkDb.Close()
		ChunkDb = oldChunkDb
	})
	return pool
}

func testFinal(chunkNum, numChunks int) *slotFinal {
	f := newSlotFinal()
	f.setNumChunks(numChunks)
	f.setChunkNum(chunkNum)
	return f
}

// TestSmtpMethodChunks checks that a two-chunk message still assembles after
// the chunk record lookup moved ahead of pooling the chunk.
func TestSmtpMethodChunks(t *testing.T) {
	pool := setupChunkDb(t)
	first := testFinal(1, 2)
	second := testFinal(2, 2)
	second.messageID = first.messageID
	if err := smtpMethod([]byte("part one"), first); err != nil {
		t.Fatalf("First chunk: %s", err)
	}
	if n := len(poolFiles(t, pool, "p")); n != 1 {
		t.Fatalf("Expected 1 pooled chunk, got %d", n)
	}
	if err := smtpMethod([]byte("part two"), second); err != nil {
		t.Fatalf("Second chunk: %s", err)
	}
	if n := len(poolFiles(t, pool, "p")); n != 0 {
		t.Fatalf("Chunks should be removed after assembly, got %d", n)
	}
	if n := len(poolFiles(t, pool, "m")); n != 1 {
		t.Fatalf("Expected 1 assembled message, got %d", n)
	}
}

// TestSmtpMethodChunkCountMismatch checks that a chunk whose header disagrees
// with the stored record for the same message is rejected without panicking.
func TestSmtpMethodChunkCountMismatch(t *testing.T) {
	pool := setupChunkDb(t)
	first := testFinal(1, 2)
	if err := smtpMethod([]byte("part one"), first); err != nil {
		t.Fatalf("First chunk: %s", err)
	}
	// Chunk 3 of 3 would index past the end of the 2-entry stored record.
	mismatched := testFinal(3, 3)
	mismatched.messageID = first.messageID
	err := smtpMethod([]byte("part two"), mismatched)
	if err == nil || !strings.Contains(err.Error(), "chunk count mismatch") {
		t.Fatalf("Expected a chunk count mismatch error, got: %v", err)
	}
	// Only the first chunk should be pooled; the rejected one is not written.
	if n := len(poolFiles(t, pool, "p")); n != 1 {
		t.Fatalf("Expected 1 pooled chunk, got %d", n)
	}
}

// TestDecodeSafelyRecovers checks that a panic during decoding becomes an
// error instead of stopping the process.  A nil secret keyring makes
// decodeMsg panic on the key lookup.
func TestDecodeSafelyRecovers(t *testing.T) {
	err := decodeSafely(make([]byte, messageBytes), nil)
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("Expected a recovered panic error, got: %v", err)
	}
}
