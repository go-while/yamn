package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crooks/yamn/config"
	"github.com/crooks/yamn/idlog"
	"github.com/crooks/yamn/keymgr"
)

// testRemailer is a remailer identity with its own secret keyring.
type testRemailer struct {
	name    string
	address string
	secring *keymgr.Secring
}

// newTestRemailer generates a keypair for a remailer and returns it along
// with its published public key, in the format used by pubring.mix.
func newTestRemailer(t *testing.T, dir, name string, exit bool) (*testRemailer, []byte) {
	t.Helper()
	r := &testRemailer{name: name, address: name + "@remailer.invalid"}
	pubkeyFile := filepath.Join(dir, name+".key.txt")
	r.secring = keymgr.NewSecring(filepath.Join(dir, name+".secring.mix"), pubkeyFile)
	r.secring.SetName(name)
	r.secring.SetAddress(r.address)
	r.secring.SetExit(exit)
	r.secring.SetValidity(14, 7)
	r.secring.SetVersion(version)
	pub, sec := eccGenerate()
	keyid := r.secring.Insert(pub, sec)
	r.secring.WritePublic(pub, keyid)
	pubkey, err := os.ReadFile(pubkeyFile)
	if err != nil {
		t.Fatal(err)
	}
	return r, pubkey
}

// setupRoundTrip points the package globals (cfg, flag, Pubring, IDDb) at a
// temporary directory holding a middle and an exit remailer.  The globals
// are restored when the test ends.
func setupRoundTrip(t *testing.T) (middle, exit *testRemailer) {
	t.Helper()
	oldCfg, oldFlag, oldPubring, oldIDDb := cfg, flag, Pubring, IDDb
	t.Cleanup(func() { cfg, flag, Pubring, IDDb = oldCfg, oldFlag, oldPubring, oldIDDb })

	dir := t.TempDir()
	// An empty config file stops ParseConfig finding a real yamn.yml.
	cfgFile := filepath.Join(dir, "yamn.yml")
	if err := os.WriteFile(cfgFile, []byte("---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	flag = &config.Flags{Dir: dir, Config: cfgFile, NoDummy: true}
	cfg, err = flag.ParseConfig()
	if err != nil {
		t.Fatal(err)
	}

	middle, middleKey := newTestRemailer(t, dir, "middle", false)
	exit, exitKey := newTestRemailer(t, dir, "exit", true)
	pubringFile := filepath.Join(dir, "pubring.mix")
	pubring := append(append(middleKey, '\n'), exitKey...)
	if err := os.WriteFile(pubringFile, pubring, 0600); err != nil {
		t.Fatal(err)
	}
	Pubring = keymgr.NewPubring(pubringFile, filepath.Join(dir, "mlist2.txt"))
	if err := Pubring.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	if Pubring.Count() != 2 {
		t.Fatalf("Expected 2 remailers in pubring, got %d", Pubring.Count())
	}

	IDDb = idlog.NewIDLog(filepath.Join(dir, "idlog"), 14)
	t.Cleanup(IDDb.Close)
	return
}

// actAs switches the global config to the given remailer and gives it an
// empty pool directory, which is returned.
func actAs(t *testing.T, r *testRemailer, exit bool) string {
	t.Helper()
	cfg.Remailer.Name = r.name
	cfg.Remailer.Address = r.address
	cfg.Remailer.Exit = exit
	cfg.Files.Pooldir = t.TempDir()
	return cfg.Files.Pooldir
}

// poolFiles returns the content of every file in dir with the given prefix.
func poolFiles(t *testing.T, dir, prefix string) [][]byte {
	t.Helper()
	names, err := readDir(dir, prefix)
	if err != nil {
		t.Fatal(err)
	}
	var files [][]byte
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, b)
	}
	return files
}

// encodeTestMsg encodes plain for the given chain of remailers.
func encodeTestMsg(plain []byte, chain ...*testRemailer) []byte {
	addresses := make([]string, len(chain))
	for n, r := range chain {
		addresses[n] = r.address
	}
	final := newSlotFinal()
	final.setNumChunks(1)
	packet, err := encodeMsg(plain, addresses, *final)
	if err != nil {
		panic(err)
	}
	return packet
}

// TestRoundTrip sends a message through middle -> exit using the real
// encodeMsg and decodeMsg, and checks the exit pools the original plaintext.
func TestRoundTrip(t *testing.T) {
	middle, exit := setupRoundTrip(t)
	plain := []byte("To: alice@example.invalid\nSubject: test\n\nHello through two hops.\n")
	packet := encodeTestMsg(plain, middle, exit)
	if len(packet) != messageBytes {
		t.Fatalf("Packet length: expected %d, got %d", messageBytes, len(packet))
	}

	// Hop 1: the middle remailer should forward an armored packet to exit.
	middlePool := actAs(t, middle, false)
	if err := decodeMsg(packet, middle.secring); err != nil {
		t.Fatalf("Middle decode failed: %s", err)
	}
	forwarded := poolFiles(t, middlePool, "m")
	if len(forwarded) != 1 {
		t.Fatalf("Middle should pool 1 outbound message, got %d", len(forwarded))
	}
	if !bytes.Contains(forwarded[0], []byte("To: "+exit.address+"\n")) {
		t.Fatalf("Forwarded message not addressed to %s:\n%s", exit.address, forwarded[0])
	}
	if bytes.Contains(forwarded[0], plain) {
		t.Fatal("Plaintext visible in the middle remailer's outbound pool")
	}
	nextPacket, err := stripArmor(bytes.NewReader(forwarded[0]))
	if err != nil {
		t.Fatalf("Unable to strip armor from forwarded message: %s", err)
	}

	// Hop 2: the exit remailer should pool the decrypted plaintext.
	exitPool := actAs(t, exit, true)
	if err := decodeMsg(nextPacket, exit.secring); err != nil {
		t.Fatalf("Exit decode failed: %s", err)
	}
	delivered := poolFiles(t, exitPool, "m")
	if len(delivered) != 1 {
		t.Fatalf("Exit should pool 1 plaintext message, got %d", len(delivered))
	}
	if !bytes.HasSuffix(delivered[0], plain) {
		t.Fatalf("Delivered message does not end with the plaintext:\n%s", delivered[0])
	}
}

// TestRoundTripReplay checks that the same packet is only processed once.
func TestRoundTripReplay(t *testing.T) {
	_, exit := setupRoundTrip(t)
	packet := encodeTestMsg([]byte("Replay me"), exit)
	pool := actAs(t, exit, true)
	if err := decodeMsg(packet, exit.secring); err != nil {
		t.Fatalf("First decode failed: %s", err)
	}
	err := decodeMsg(packet, exit.secring)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("Replayed packet should be rejected with a collision, got: %v", err)
	}
	if n := len(poolFiles(t, pool, "m")); n != 1 {
		t.Fatalf("Replayed packet should not be delivered twice, got %d files", n)
	}
}

// TestRoundTripTampered checks that a packet modified in transit is dropped.
func TestRoundTripTampered(t *testing.T) {
	_, exit := setupRoundTrip(t)
	packet := encodeTestMsg([]byte("Do not tamper"), exit)
	// Flip a bit in the body, which is covered by the anti-tag digest.
	packet[len(packet)-1] ^= 0x01
	pool := actAs(t, exit, true)
	decodeMsg(packet, exit.secring)
	if n := len(poolFiles(t, pool, "")); n != 0 {
		t.Fatalf("Tampered packet should not be pooled, got %d files", n)
	}
}

// TestRoundTripWrongKey checks that a remailer cannot decode a packet that
// was encrypted to a different remailer.
func TestRoundTripWrongKey(t *testing.T) {
	middle, exit := setupRoundTrip(t)
	packet := encodeTestMsg([]byte("Not for you"), exit)
	pool := actAs(t, middle, true)
	decodeMsg(packet, middle.secring)
	if n := len(poolFiles(t, pool, "")); n != 0 {
		t.Fatalf("Packet for another remailer should not be pooled, got %d files", n)
	}
}
