package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
)

// countingReader counts the bytes stripArmor reads.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func armoredPacket(t *testing.T) []byte {
	t.Helper()
	payload := make([]byte, messageBytes)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	armor(&b, payload)
	return b.Bytes()
}

// slack is what bufio.Scanner may read ahead of the line it returns.
const slack = 64*1024 + 4096

// A message with far more base64 than a packet is refused once it passes
// the size of a packet, instead of being collected in memory.
func TestStripArmorOversizedPayload(t *testing.T) {
	head := "::\nRemailer-Type: yamn-test\n\n-----BEGIN REMAILER MESSAGE-----\n20480\n" +
		strings.Repeat("0", 64) + "\n"
	line := strings.Repeat("A", 76) + "\n"
	body := strings.Repeat(line, 4<<20/len(line)) // 4 MB
	msg := head + body + "-----END REMAILER MESSAGE-----\n"
	r := &countingReader{r: strings.NewReader(msg)}
	_, err := stripArmor(r)
	if err == nil || !strings.Contains(err.Error(), "longer than a yamn packet") {
		t.Fatalf("err = %v, want a payload size error", err)
	}
	if max := len(head) + base64.StdEncoding.EncodedLen(messageBytes) + slack; r.n > max {
		t.Errorf("read %d bytes of a %d byte message, want at most %d", r.n, len(msg), max)
	}
}

// Nothing after the End cutmark is read.
func TestStripArmorStopsAtEndCutmark(t *testing.T) {
	packet := armoredPacket(t)
	trailer := bytes.Repeat([]byte("trailing text after the packet\n"), 64<<10) // 2 MB
	r := &countingReader{r: io.MultiReader(bytes.NewReader(packet), bytes.NewReader(trailer))}
	if _, err := stripArmor(r); err != nil {
		t.Fatalf("stripArmor: %v", err)
	}
	if max := len(packet) + slack; r.n > max {
		t.Errorf("read %d bytes, want at most %d (the packet is %d)", r.n, max, len(packet))
	}
}

// A read error is reported as such, not as a missing cutmark.
func TestStripArmorReadError(t *testing.T) {
	packet := armoredPacket(t)
	broken := errors.New("disk on fire")
	r := io.MultiReader(bytes.NewReader(packet[:len(packet)/2]), errReader{broken})
	_, err := stripArmor(r)
	if !errors.Is(err, broken) {
		t.Fatalf("err = %v, want %v", err, broken)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// A message that ends right after the Begin cutmark, or after the size,
// says what is missing.
func TestStripArmorTruncatedHeader(t *testing.T) {
	for _, tc := range []struct{ msg, want string }{
		{"::\n\n-----BEGIN REMAILER MESSAGE-----\n", "no payload size"},
		{"::\n\n-----BEGIN REMAILER MESSAGE-----\n20480\n", "no payload hash"},
	} {
		_, err := stripArmor(strings.NewReader(tc.msg))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want %q", tc.msg, err, tc.want)
		}
	}
}

// A packet as armor writes it still comes through.
func TestStripArmorValidPacket(t *testing.T) {
	payload := make([]byte, messageBytes)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	armor(&b, payload)
	got, err := stripArmor(&b)
	if err != nil {
		t.Fatalf("stripArmor: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("payload changed")
	}
}
