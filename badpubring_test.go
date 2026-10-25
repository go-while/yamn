package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crooks/yamn/keymgr"
)

// setupBadAddressRing builds a middle remailer and an exit remailer whose
// pubring address has no "@" (name "bad", address "bad.remailer.invalid").
// Its keyid matches its public key.  Stats make "bad" the only exit
// candidate.
func setupBadAddressRing(t *testing.T) (middle *testRemailer) {
	t.Helper()
	setupTestConfig(t)
	oldPubring, oldIDDb := Pubring, IDDb
	t.Cleanup(func() { Pubring, IDDb = oldPubring, oldIDDb })
	dir := t.TempDir()
	middle, middleKey := newTestRemailer(t, dir, "middle", false)
	_, badKey := newTestRemailer(t, dir, "bad", true)
	badKey = bytes.Replace(badKey, []byte("bad@remailer.invalid"), []byte("bad.remailer.invalid"), 1)
	cfg.Files.Pubring = filepath.Join(dir, "pubring.mix")
	if err := os.WriteFile(cfg.Files.Pubring, append(append(middleKey, '\n'), badKey...), 0600); err != nil {
		t.Fatal(err)
	}
	var stats strings.Builder
	stats.WriteString("Stats-Version: 2.0\n")
	fmt.Fprintf(&stats, "Generated: %s\n", time.Now().UTC().Format("Mon 02 Jan 2006 15:04:05 GMT"))
	stats.WriteString("Mixmaster    Latent-Hist   Latent  Uptime-Hist   Uptime  Options\n")
	stats.WriteString(strings.Repeat("-", 64) + "\n")
	for _, name := range []string{"middle", "bad"} {
		fmt.Fprintf(&stats, "%-12s %s    :10   %s  100.0%%\n", name, strings.Repeat("0", 12), strings.Repeat("+", 12))
	}
	stats.WriteString("\n")
	cfg.Files.Mlist2 = filepath.Join(dir, "mlist2.txt")
	if err := os.WriteFile(cfg.Files.Mlist2, []byte(stats.String()), 0600); err != nil {
		t.Fatal(err)
	}
	Pubring = keymgr.NewPubring(cfg.Files.Pubring, cfg.Files.Mlist2)
	if err := Pubring.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	IDDb = openTestIDLog(t, filepath.Join(dir, "idlog"), 16)
	t.Cleanup(IDDb.Close)
	return middle
}

// TestRandhopBadPubringAddress checks that a middle remailer survives
// randhopping an exit packet while the pubring has an entry whose address
// has no "@".  It used to pick that entry and exit (status 1) from
// encodeMsg, which decodeSafely can't catch.  The test runs the remailer in
// a child process.
func TestRandhopBadPubringAddress(t *testing.T) {
	if os.Getenv("YAMN_TEST_RANDHOP_CHILD") == "1" {
		middle := setupBadAddressRing(t)
		flag.Remailer = true // as in loopServer
		actAs(t, middle, false)
		// An exit (type 1) packet addressed to the middle remailer,
		// which then randhops it.
		packet := encodeTestMsg([]byte("To: a@example.invalid\n\nhi\n"), middle)
		err := decodeSafely(packet, middle.secring)
		fmt.Printf("CHILD-SURVIVED err=%v\n", err)
		return
	}
	setupBadAddressRing(t)
	if Pubring.Count() != 1 {
		t.Errorf("the entry without @ was imported: %d keys", Pubring.Count())
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRandhopBadPubringAddress$", "-test.v")
	cmd.Env = append(os.Environ(), "YAMN_TEST_RANDHOP_CHILD=1")
	out, err := cmd.CombinedOutput()
	if !bytes.Contains(out, []byte("CHILD-SURVIVED")) {
		t.Errorf("remailer process stopped while decoding one packet (%v):\n%s", err, out)
	}
}

// TestLongPubringAddress checks that an address longer than the 52 bytes
// of the next-hop field isn't imported, so encodeMsg can't panic on it.
func TestLongPubringAddress(t *testing.T) {
	setupTestConfig(t)
	oldPubring := Pubring
	t.Cleanup(func() { Pubring = oldPubring })
	dir := t.TempDir()
	_, aKey := newTestRemailer(t, dir, "first", false)
	long := strings.Repeat("x", 50) + "@remailer.invalid"
	_, bKey := newTestRemailer(t, dir, "long", true)
	bKey = bytes.Replace(bKey, []byte("long@remailer.invalid"), []byte(long), 1)
	cfg.Files.Pubring = filepath.Join(dir, "pubring.mix")
	os.WriteFile(cfg.Files.Pubring, append(append(aKey, '\n'), bKey...), 0600)
	Pubring = keymgr.NewPubring(cfg.Files.Pubring, filepath.Join(dir, "mlist2.txt"))
	if err := Pubring.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	if _, err := Pubring.Get(long); err == nil {
		t.Errorf("%d-byte address imported", len(long))
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("encodeMsg panics: %v", r)
		}
	}()
	final := newSlotFinal()
	if _, err := encodeMsg([]byte("x"), []string{"first@remailer.invalid", long}, *final); err == nil {
		t.Error("encodeMsg accepted a hop that isn't in the keyring")
	}
}

// TestEncodeMsgUnknownHop checks that encodeMsg returns an error for a hop
// that isn't in the keyring, instead of exiting.  It runs in a child
// process, so the old os.Exit shows up as a failure.
func TestEncodeMsgUnknownHop(t *testing.T) {
	if os.Getenv("YAMN_TEST_ENCODE_CHILD") == "1" {
		middle, exit := setupRoundTrip(t)
		final := newSlotFinal()
		for _, chain := range [][]string{
			{middle.address, "nobody@remailer.invalid"}, // unknown exit
			{"nobody@remailer.invalid", exit.address},   // unknown middle
		} {
			if _, err := encodeMsg([]byte("x"), chain, *final); err == nil {
				fmt.Printf("NO-ERROR %v\n", chain)
			}
		}
		fmt.Println("CHILD-SURVIVED")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestEncodeMsgUnknownHop$", "-test.v")
	cmd.Env = append(os.Environ(), "YAMN_TEST_ENCODE_CHILD=1")
	out, err := cmd.CombinedOutput()
	if !bytes.Contains(out, []byte("CHILD-SURVIVED")) || bytes.Contains(out, []byte("NO-ERROR")) {
		t.Errorf("encodeMsg with an unknown hop (%v):\n%s", err, out)
	}
}
