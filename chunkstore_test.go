package main

import (
	"bytes"
	"testing"
	"time"
)

// TestDuplicateChunkKeepsFirst checks that a second copy of a chunk (same
// message ID, another packet ID) is dropped: the first copy is assembled and
// no chunk file is left behind.
func TestDuplicateChunkKeepsFirst(t *testing.T) {
	pool := setupChunkDb(t)
	first := testFinal(1, 2)
	dup := testFinal(1, 2)
	dup.messageID = first.messageID
	second := testFinal(2, 2)
	second.messageID = first.messageID
	if err := smtpMethod([]byte("part one|"), first); err != nil {
		t.Fatal(err)
	}
	smtpMethod([]byte("part one again|"), dup)
	if n := len(poolFiles(t, pool, "p")); n != 1 {
		t.Errorf("a duplicate chunk should not be stored, %d chunk files", n)
	}
	if err := smtpMethod([]byte("part two"), second); err != nil {
		t.Fatal(err)
	}
	if n := len(poolFiles(t, pool, "p")); n != 0 {
		t.Errorf("%d orphaned chunk files left after assembly", n)
	}
	delivered := poolFiles(t, pool, "m")
	if len(delivered) != 1 || !bytes.HasSuffix(delivered[0], []byte("\npart one|part two")) {
		t.Errorf("the first copy of the chunk should be assembled, got %q", delivered)
	}
}

// TestChunkExpiryNotExtended checks that later chunks of a message don't
// move the expiry date of its record.
func TestChunkExpiryNotExtended(t *testing.T) {
	setupChunkDb(t)
	first := testFinal(1, 3)
	second := testFinal(2, 3)
	second.messageID = first.messageID
	ChunkDb.SetExpire(2)
	if err := smtpMethod([]byte("one"), first); err != nil {
		t.Fatal(err)
	}
	ChunkDb.SetExpire(60)
	if err := smtpMethod([]byte("two"), second); err != nil {
		t.Fatal(err)
	}
	record, err := ChunkDb.db.Get(first.getMessageID(), nil)
	if err != nil {
		t.Fatal(err)
	}
	stamp, _ := chunkItems(record)
	if want := time.Now().Add(2 * 24 * time.Hour).Format("20060102"); stamp != want {
		t.Errorf("record expires %s, want %s (set by the first chunk)", stamp, want)
	}
}

// TestPartialChunkLimit checks that remailer.chunk_limit caps the partial chunks
// kept, and that a chunk completing a message is still accepted.
func TestPartialChunkLimit(t *testing.T) {
	pool := setupChunkDb(t)
	cfg.Remailer.ChunkLimit = 3
	// Two messages of three chunks, two chunks of each sent.
	a1, a2, a3 := testFinal(1, 3), testFinal(2, 3), testFinal(3, 3)
	a2.messageID, a3.messageID = a1.messageID, a1.messageID
	b1, b2 := testFinal(1, 3), testFinal(2, 3)
	b2.messageID = b1.messageID
	for _, f := range []*slotFinal{a1, a2, b1} {
		if err := smtpMethod([]byte("chunk"), f); err != nil {
			t.Fatal(err)
		}
	}
	if err := smtpMethod([]byte("chunk"), b2); err == nil {
		t.Error("a chunk beyond chunk_limit should be refused")
	}
	if n := len(poolFiles(t, pool, "p")); n != 3 {
		t.Errorf("expected 3 stored chunks, got %d", n)
	}
	if err := smtpMethod([]byte("chunk"), a3); err != nil {
		t.Errorf("a chunk completing a message should be accepted at the limit: %s", err)
	}
	if n := len(poolFiles(t, pool, "m")); n != 1 {
		t.Errorf("the completed message should be assembled, %d pooled", n)
	}
}
