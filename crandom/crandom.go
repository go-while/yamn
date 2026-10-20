package crandom

import (
	urand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math/rand"
)

// The following section defines crypto/rand as a source for functions in
// math/rand.  This means we can use many of the math/rand functions with
// a cryptographically random source.

// cryptoRandSource is an empty struct for creating a new random source in the
// rand package.
type cryptoRandSource struct{}

// newCryptoRandSource returns a new instance of cryptoRandSource.
func newCryptoRandSource() cryptoRandSource {
	return cryptoRandSource{}
}

func (cryptoRandSource) Int63() int64 {
	var b [8]byte
	urand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]) & (1<<63 - 1))
}

func (cryptoRandSource) Seed(_ int64) {}

// And so ends the random magic section

// Randbytes returns n Bytes of random data
func Randbytes(n int) (b []byte) {
	b = make([]byte, n)
	read, err := urand.Read(b)
	if err != nil {
		panic(err)
	}
	if read != n {
		err = fmt.Errorf(
			"Insufficient entropy.  Wanted=%d, Got=%d",
			n,
			read,
		)
		panic(err)
	}
	return
}

// Dice returns a random integer of range 0-255
func Dice() int {
	var b [1]byte
	urand.Read(b[:])
	return int(b[0])
}

// RandomInt returns an integer between 0 and max
func RandomInt(max int) int {
	r := rand.New(newCryptoRandSource())
	return r.Intn(max)
}

// RandInts returns the integers 0 to n-1 in random order.  Every ordering is
// equally likely.
func RandInts(n int) []int {
	return rand.New(newCryptoRandSource()).Perm(n)
}

// Shuffle puts a string slice in random order (Fisher-Yates).  Every ordering
// is equally likely.
func Shuffle(slice []string) {
	r := rand.New(newCryptoRandSource())
	r.Shuffle(len(slice), func(i, j int) {
		slice[i], slice[j] = slice[j], slice[i]
	})
}
