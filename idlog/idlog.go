package idlog

import (
	"errors"
	"github.com/syndtr/goleveldb/leveldb"
	"time"
)

type IDLog struct {
	db       *leveldb.DB   // A level DB instance
	validity time.Duration // Days before an entry is expired
}

func NewIDLog(filename string, days int) *IDLog {
	if days < 1 {
		err := errors.New("Invalid validity days. Must be 1 or more")
		panic(err)
	}
	iddb, err := leveldb.OpenFile(filename, nil)
	if err != nil {
		panic(err)
	}
	return &IDLog{
		db:       iddb,
		validity: time.Duration(24*days) * time.Hour,
	}
}

func (i *IDLog) Close() {
	i.db.Close()
}

// Unique tests the existance of a key and inserts if it's not there.
// The data inserted is a Gob'd expiry date
func (i *IDLog) Unique(key []byte) (unique bool) {
	var err error
	_, err = i.db.Get(key, nil)
	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			// This condition indicates we don't know this key
			unique = true
			i.put(key)
		} else {
			// It's not an error we anticipated
			panic(err)
		}
	} else {
		/*
			The DB already contains the key we're trying to insert.
			This implies that we've already processed this packet
			and don't want to process it again.
		*/
		unique = false
	}
	return
}

// Forget deletes key, so a packet that couldn't be processed because of a
// local error can be processed again.
func (i *IDLog) Forget(key []byte) {
	i.db.Delete(key, nil)
}

// put stores key with an expiry date one validity period from now.
func (i *IDLog) put(key []byte) {
	insertTimestamp, err := time.Now().Add(i.validity).GobEncode()
	if err != nil {
		panic(err)
	}
	err = i.db.Put(key, insertTimestamp, nil)
	if err != nil {
		panic(err)
	}
}

// Expire deletes expired entries.  An entry whose expiry date can't be
// read gets a new one, a full validity period from now, so its packet ID
// stays known; bad counts them.
func (i *IDLog) Expire() (count, deleted, bad int) {
	var err error
	now := time.Now()
	iter := i.db.NewIterator(nil, nil)
	var timestamp time.Time
	for iter.Next() {
		key := iter.Key()
		err = timestamp.GobDecode(iter.Value())
		if err != nil {
			i.put(key)
			bad++
			count++
			continue
		}
		if now.After(timestamp) {
			i.db.Delete(key, nil)
			deleted++
		} else {
			count++
		}
	}
	iter.Release()
	err = iter.Error()
	if err != nil {
		panic(err)
	}
	return
}
