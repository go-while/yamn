package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crooks/yamn/keymgr"
)

// deliverPool decodes every outbound message in fromPool as remailer r and
// returns r's pool directory.  It stands in for the mail transport between
// hops.
func deliverPool(t *testing.T, fromPool string, r *testRemailer, exit bool) string {
	t.Helper()
	pool := actAs(t, r, exit)
	for _, f := range poolFiles(t, fromPool, "m") {
		packet, err := stripArmor(bytes.NewReader(f))
		if err != nil {
			t.Fatalf("Unable to strip armor: %s", err)
		}
		// Duplicate copies are expected to be rejected, so errors here are
		// only logged.
		if err := decodeMsg(packet, r.secring); err != nil {
			t.Logf("%s: %s", r.name, err)
		}
	}
	return pool
}

// TestMultiChunkMessage sends a message that needs three chunks through the
// real client (mixprep), a middle and an exit remailer, with and without
// multiple copies, and checks the exit delivers the original message once.
func TestMultiChunkMessage(t *testing.T) {
	for _, copies := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d copies", copies), func(t *testing.T) {
			middle, exit := setupRoundTrip(t)
			setupChunkDbOnly(t)
			cfg.Urls.Fetch = false
			flag.Chain = middle.address + "," + exit.address
			flag.Copies = copies

			// A message body long enough for three chunks, with a line
			// number on every line so any corruption shows up.
			var body strings.Builder
			for n := 0; body.Len() < 2*maxFragLength+1000; n++ {
				fmt.Fprintf(&body, "Line %05d of the multi-chunk test message.\n", n)
			}
			msgFile := filepath.Join(t.TempDir(), "message.txt")
			msg := "To: alice@example.invalid\nSubject: chunks\n\n" + body.String()
			if err := os.WriteFile(msgFile, []byte(msg), 0600); err != nil {
				t.Fatal(err)
			}
			flag.Args = []string{msgFile}

			// The client pools one packet per chunk and copy.
			clientPool := t.TempDir()
			cfg.Files.Pooldir = clientPool
			mixprep()
			if n := len(poolFiles(t, clientPool, "m")); n != 3*copies {
				t.Fatalf("Client should pool %d packets, got %d", 3*copies, n)
			}

			middlePool := deliverPool(t, clientPool, middle, false)
			exitPool := deliverPool(t, middlePool, exit, true)

			delivered := poolFiles(t, exitPool, "m")
			if len(delivered) != 1 {
				t.Fatalf("Exit should deliver 1 assembled message, got %d (partial chunks left: %d)",
					len(delivered), len(poolFiles(t, exitPool, "p")))
			}
			if n := bytes.Count(delivered[0], []byte("Yamn-Pooled-Date:")); n != 1 {
				t.Errorf("Assembled message should have 1 internal header, got %d", n)
			}
			if !bytes.HasSuffix(delivered[0], []byte(body.String())) {
				t.Error("Assembled message does not end with the original body")
			}
		})
	}
}

// setupRandomExits creates a middle and two exit remailers, with a pubring
// and remailer stats so the client can pick a random exit ("*").
func setupRandomExits(t *testing.T) (middle *testRemailer, exits []*testRemailer) {
	t.Helper()
	setupTestConfig(t)
	oldPubring, oldIDDb := Pubring, IDDb
	t.Cleanup(func() { Pubring, IDDb = oldPubring, oldIDDb })
	dir := t.TempDir()

	var pubring []byte
	add := func(name string, exit bool) *testRemailer {
		r, key := newTestRemailer(t, dir, name, exit)
		pubring = append(append(pubring, key...), '\n')
		return r
	}
	middle = add("middle", false)
	exits = []*testRemailer{add("exita", true), add("exitb", true)}
	cfg.Files.Pubring = filepath.Join(dir, "pubring.mix")
	if err := os.WriteFile(cfg.Files.Pubring, pubring, 0600); err != nil {
		t.Fatal(err)
	}

	// Stats with good latency and uptime for every remailer.
	var stats strings.Builder
	stats.WriteString("Stats-Version: 2.0\n")
	fmt.Fprintf(&stats, "Generated: %s\n", time.Now().UTC().Format("Mon 02 Jan 2006 15:04:05 GMT"))
	stats.WriteString("Mixmaster    Latent-Hist   Latent  Uptime-Hist   Uptime  Options\n")
	stats.WriteString(strings.Repeat("-", 64) + "\n")
	for _, name := range []string{"middle", "exita", "exitb"} {
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
	IDDb = openTestIDLog(t, filepath.Join(dir, "idlog"), 14)
	t.Cleanup(IDDb.Close)
	return
}

// TestMultiChunkRandomExit checks that with a random exit, all chunks of a
// message go to the same exit remailer, which then assembles them.
func TestMultiChunkRandomExit(t *testing.T) {
	middle, exits := setupRandomExits(t)
	setupChunkDbOnly(t)
	cfg.Urls.Fetch = false
	flag.Chain = middle.address + ",*"
	flag.Copies = 1

	// Six chunks: if each chunk picked its exit independently, they'd all
	// land on the same one only 1 time in 32.
	var body strings.Builder
	for n := 0; body.Len() < 5*maxFragLength+1000; n++ {
		fmt.Fprintf(&body, "Line %05d of the random exit test message.\n", n)
	}
	msgFile := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(msgFile, []byte("To: alice@example.invalid\n\n"+body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	flag.Args = []string{msgFile}
	clientPool := t.TempDir()
	cfg.Files.Pooldir = clientPool
	mixprep()

	middlePool := deliverPool(t, clientPool, middle, false)
	forwarded := poolFiles(t, middlePool, "m")
	if len(forwarded) != 6 {
		t.Fatalf("Middle should forward 6 chunks, got %d", len(forwarded))
	}
	var exit *testRemailer
	for _, e := range exits {
		if bytes.Contains(forwarded[0], []byte("To: "+e.address+"\n")) {
			exit = e
		}
	}
	if exit == nil {
		t.Fatalf("First chunk not addressed to a known exit:\n%.200s", forwarded[0])
	}
	for n, f := range forwarded {
		if !bytes.Contains(f, []byte("To: "+exit.address+"\n")) {
			t.Fatalf("Chunk %d not sent to the same exit (%s)", n+1, exit.address)
		}
	}

	exitPool := deliverPool(t, middlePool, exit, true)
	delivered := poolFiles(t, exitPool, "m")
	if len(delivered) != 1 || !bytes.HasSuffix(delivered[0], []byte(body.String())) {
		t.Fatalf("Exit should deliver the original message once, got %d messages", len(delivered))
	}
}
