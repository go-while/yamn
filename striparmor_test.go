package main

import (
	"bufio"
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

// The cap is exactly a packet's base64: one byte more is refused before
// decoding, exactly that much gets as far as the digest check.
func TestStripArmorCapBoundary(t *testing.T) {
	head := "::\n\n-----BEGIN REMAILER MESSAGE-----\n20480\n" + strings.Repeat("0", 64) + "\n"
	wrap := func(b64 string) string {
		var b strings.Builder
		for len(b64) > 64 {
			b.WriteString(b64[:64] + "\n")
			b64 = b64[64:]
		}
		b.WriteString(b64 + "\n")
		return b.String()
	}
	packet := base64.StdEncoding.EncodeToString(make([]byte, messageBytes))
	_, err := stripArmor(strings.NewReader(head + wrap(packet+"A") + "-----END REMAILER MESSAGE-----\n"))
	if err == nil || !strings.Contains(err.Error(), "longer than a yamn packet") {
		t.Errorf("%d base64 bytes: err = %v, want the size error", len(packet)+1, err)
	}
	_, err = stripArmor(strings.NewReader(head + wrap(packet) + "-----END REMAILER MESSAGE-----\n"))
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Errorf("%d base64 bytes: err = %v, want a digest error", len(packet), err)
	}
}

// Armor with CRLF line ends, or without a newline after the End
// cutmark, still decodes; a line over bufio.Scanner's limit is reported
// as a read error, not as a missing cutmark.
func TestStripArmorLineEnds(t *testing.T) {
	payload := make([]byte, messageBytes)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	armor(&b, payload)
	crlf := strings.ReplaceAll(b.String(), "\n", "\r\n")
	noFinalNL := strings.TrimSuffix(b.String(), "\n")
	for name, msg := range map[string]string{"CRLF": crlf, "no final newline": noFinalNL} {
		got, err := stripArmor(strings.NewReader(msg))
		if err != nil || !bytes.Equal(got, payload) {
			t.Errorf("%s: err = %v, payload equal %v", name, err, bytes.Equal(got, payload))
		}
	}
	long := "::\n\n-----BEGIN REMAILER MESSAGE-----\n20480\n" + strings.Repeat("0", 64) + "\n" +
		strings.Repeat("A", 70000) + "\n-----END REMAILER MESSAGE-----\n"
	_, err := stripArmor(strings.NewReader(long))
	if err == nil {
		t.Fatal("a 70000 byte line was accepted")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("err = %v, want bufio.ErrTooLong", err)
	}
}

// lastReadErr returns its data, and with the last of it a read error, in
// the same Read: the scanner then holds the error while it still returns
// the lines it read, End cutmark included.
type lastReadErr struct {
	data    []byte
	err     error
	gaveErr bool
}

func (r *lastReadErr) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		r.gaveErr = true
		return n, r.err
	}
	return n, nil
}

// A read error that comes with the End cutmark doesn't cost a complete
// packet.
func TestStripArmorErrorAfterEnd(t *testing.T) {
	payload := make([]byte, messageBytes)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	armor(&b, payload)
	r := &lastReadErr{data: b.Bytes(), err: errors.New("disk on fire")}
	got, err := stripArmor(r)
	if !r.gaveErr {
		t.Fatal("the read error was never returned")
	}
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("err = %v, payload equal %v", err, bytes.Equal(got, payload))
	}
}
