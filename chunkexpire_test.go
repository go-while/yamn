package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestChunkItems(t *testing.T) {
	stamp, items := chunkItems([]byte("20261020,pchunk1,pchunk2"))
	if stamp != "20261020" {
		t.Errorf("stamp: got %q", stamp)
	}
	if !slices.Equal(items, []string{"pchunk1", "pchunk2"}) {
		t.Errorf("items: got %q, want the two chunk files once each", items)
	}
}

func TestChunkExpire(t *testing.T) {
	pool := setupChunkDb(t)
	touch := func(name string) {
		if err := os.WriteFile(filepath.Join(pool, name), []byte("chunk"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"pold1", "pold2", "pbad", "pkeep"} {
		touch(f)
	}
	// An expired record (expiry two days ago), one kept for five more days,
	// and one with a corrupt timestamp.
	ChunkDb.SetExpire(-2)
	ChunkDb.Insert([]byte("old"), []string{"pold1", "pold2"})
	ChunkDb.SetExpire(5)
	ChunkDb.Insert([]byte("keep"), []string{"pkeep", ""})
	if err := ChunkDb.db.Put([]byte("bad"), []byte("notadate,pbad"), nil); err != nil {
		t.Fatal(err)
	}

	retained, deleted := ChunkDb.Expire()
	if retained != 1 || deleted != 2 {
		t.Errorf("Expire: got retained=%d deleted=%d, want 1 and 2", retained, deleted)
	}
	left := poolFiles(t, pool, "p")
	if len(left) != 1 {
		t.Errorf("Only pkeep should remain in the pool, %d files left", len(left))
	}
	for _, key := range []string{"old", "bad"} {
		if items := ChunkDb.Get([]byte(key), 2); items[0] != "" || items[1] != "" {
			t.Errorf("Record %q should be deleted, got %q", key, items)
		}
	}
	if items := ChunkDb.Get([]byte("keep"), 2); items[0] != "pkeep" {
		t.Errorf("Record \"keep\" should be retained, got %q", items)
	}
}
