package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Masterminds/log-go"
	"github.com/syndtr/goleveldb/leveldb"
)

// Chunk holds variables that apply globally to all chunked message content.
type Chunk struct {
	db         *leveldb.DB   // A level DB instance
	expireDays time.Duration // How long to retain keys
	deleteDays time.Duration // Age of partial files before deletion
}

// OpenChunk opens a levelDB database file
func OpenChunk(filename string) *Chunk {
	levelDB, err := leveldb.OpenFile(filename, nil)
	if err != nil {
		panic(err)
	}
	return &Chunk{db: levelDB}
}

// Close closes the levelDB
func (chunk *Chunk) Close() {
	chunk.db.Close()
}

// SetExpire defines how long keys should be retained in the DB
func (chunk *Chunk) SetExpire(days int) {
	chunk.expireDays = time.Duration(days*24) * time.Hour
	chunk.deleteDays = time.Duration(days*24*2) * time.Hour
}

// Get returns the content associated with messageID.  If messageID is
// unknown, an empty slice of len numChunks is returned.
func (chunk *Chunk) Get(messageID []byte, numChunks int) (items []string) {
	var err error
	var content []byte
	content, err = chunk.db.Get(messageID, nil)
	if err != nil {
		if err.Error() == "leveldb: not found" {
			items = make([]string, numChunks)
		} else {
			panic(err)
		}
	} else {
		_, items = chunkItems(content)
	}
	return
}

// chunkItems splits a chunk DB record into its expiry date stamp (the first
// field, only used internally) and the chunk filenames.
func chunkItems(record []byte) (stamp string, items []string) {
	fields := strings.Split(string(record), ",")
	return fields[0], fields[1:]
}

// Insert writes an entry to the DB.  A new entry expires after expireDays;
// updating an entry keeps its expiry date, so later chunks can't extend it.
func (chunk *Chunk) Insert(messageid []byte, items []string) {
	if chunk.expireDays == 0 {
		panic("Expiry duration not defined")
	}
	stamp := time.Now().Add(chunk.expireDays).Format("20060102")
	if old, err := chunk.db.Get(messageid, nil); err == nil {
		stamp, _ = chunkItems(old)
	}
	chunk.db.Put(
		messageid,
		[]byte(fmt.Sprintf(
			"%s,%s",
			stamp,
			strings.Join(items, ","),
		)),
		nil,
	)
}

// countEmpty returns the number of empty slots (chunks not received yet)
// in items.
func countEmpty(items []string) (n int) {
	for _, item := range items {
		if item == "" {
			n++
		}
	}
	return
}

// Count returns the number of partial chunk files in the pool.
func (chunk *Chunk) Count() int {
	files, err := readDir(cfg.Files.Pooldir, "p")
	if err != nil {
		return 0
	}
	return len(files)
}

// IsPopulated returns true if all elements of items are populated
func IsPopulated(items []string) bool {
	for _, n := range items {
		if n == "" {
			return false
		}
	}
	return true
}

// Housekeep deletes files over a given age
func (chunk *Chunk) Housekeep() (ret, del int) {
	files, err := ioutil.ReadDir(cfg.Files.Pooldir)
	if err != nil {
		log.Warnf("Chunk housekeeping failed: %s", err)
		return
	}
	expire := time.Now().Add(-chunk.deleteDays)
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), "p") {
			// Don't do anything unless the file begins with "p"
			continue
		}
		if file.ModTime().Before(expire) {
			os.Remove(path.Join(cfg.Files.Pooldir, file.Name()))
			del++
		} else {
			ret++
		}
	}
	return
}

// Assemble takes all the file chunks, assembles them in order and stores the
// result back into the pool.
func (chunk *Chunk) Assemble(filename string, items []string) (err error) {
	f, err := os.Create(filename)
	if err != nil {
		return
	}
	defer f.Close()
	// Each chunk file starts with its own internal header.  Write one for the
	// assembled message and strip them from the chunks, otherwise they end up
	// in the middle of the delivered message.
	writeInternalHeader(f)
	var content []byte
	for _, c := range items {
		infile := path.Join(cfg.Files.Pooldir, c)
		content, err = ioutil.ReadFile(infile)
		if err != nil {
			log.Warnf("Chunk assembler says: %s", err)
			continue
		}
		f.Write(stripInternalHeader(content))
		err = os.Remove(infile)
		if err != nil {
			log.Warnf("Assembler chunk delete failed: %s", err)
			continue
		}
	}
	return
}

// AssembledSize returns the size of the message the chunk files in items
// would assemble into, without their internal headers.
func (chunk *Chunk) AssembledSize(items []string) (size int) {
	for _, c := range items {
		content, err := ioutil.ReadFile(path.Join(cfg.Files.Pooldir, c))
		if err != nil {
			continue
		}
		size += len(stripInternalHeader(content))
	}
	return
}

// stripInternalHeader removes the Yamn-Pooled-Date line that writePlainToPool
// puts at the start of a pool file.
func stripInternalHeader(content []byte) []byte {
	if !bytes.HasPrefix(content, []byte("Yamn-Pooled-Date: ")) {
		return content
	}
	if i := bytes.IndexByte(content, '\n'); i >= 0 {
		return content[i+1:]
	}
	return nil
}

// Delete removes the specified Message ID from the DB
func (chunk *Chunk) Delete(messageid []byte) {
	err := chunk.db.Delete(messageid, nil)
	if err != nil {
		log.Warnf("Could not delete MsgID: %s. %s", messageid, err)
	}
}

// DeleteItems removes all the filename defined in items
func (chunk *Chunk) DeleteItems(items []string) (deleted, failed int) {
	for _, file := range items {
		fqfn := path.Join(cfg.Files.Pooldir, file)
		err := os.Remove(fqfn)
		if err != nil {
			failed++
		} else {
			deleted++
		}
	}
	return
}

// Expire iterates the DB and deletes entries (and files) that exceed the
// defined age.
func (chunk *Chunk) Expire() (retained, deleted int) {
	now := time.Now()
	iter := chunk.db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		key := iter.Key()
		stamp, items := chunkItems(iter.Value())
		expire, err := time.Parse("20060102", stamp)
		if err != nil {
			log.Warnf("Could not parse timestamp: %s", err)
			// If the timestamp is invalid, delete the record
			chunk.DeleteItems(items)
			chunk.Delete(key)
			deleted++
			continue
		}
		if expire.Before(now) {
			chunk.DeleteItems(items)
			chunk.Delete(key)
			deleted++
		} else {
			retained++
		}
	}
	return
}
