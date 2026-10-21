package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// chunkLimitMessage writes a message whose assembled plaintext is size
// bytes long and makes it the client's input.
func chunkLimitMessage(t *testing.T, size int) {
	t.Helper()
	const head = "To: alice@example.invalid\n\n"
	msgFile := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(msgFile, []byte(head+strings.Repeat("x", size-len(head))), 0600); err != nil {
		t.Fatal(err)
	}
	flag.Args = []string{msgFile}
}

// runClientChild runs the test named name again in a child process with
// YAMN_CHILD set to dir, for client code that ends with os.Exit.
func runClientChild(t *testing.T, name, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	cmd.Env = append(os.Environ(), "YAMN_CHILD="+dir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestChunkLimit checks that a message needing more than maxChunks chunks
// is refused before anything is pooled.  The chunk count is a single byte
// in the packet, so chunk 256 used to wrap: part of the message was pooled
// as a complete message and the client panicked.
func TestChunkLimit(t *testing.T) {
	if dir := os.Getenv("YAMN_CHILD"); dir != "" {
		_, exit := setupRoundTrip(t)
		cfg.Urls.Fetch = false
		cfg.Files.Pooldir = dir
		flag.Chain = exit.address
		flag.Copies = 1
		// 257 chunks: the count wraps to 1, so chunk 1 alone looks complete.
		chunkLimitMessage(t, (maxChunks+1)*maxFragLength+1)
		mixprep()
		os.Exit(0)
	}
	dir := t.TempDir()
	out, err := runClientChild(t, "TestChunkLimit", dir)
	if err == nil {
		t.Errorf("A message over %d chunks should fail, got exit status 0", maxChunks)
	}
	if !strings.Contains(out, "Message too large") {
		t.Errorf("No error message about the size:\n%s", out)
	}
	if n := len(poolFiles(t, dir, "m")); n != 0 {
		t.Errorf("%d packets pooled for a message that was refused", n)
	}
}

// TestChunkLimitExact checks that a message of exactly maxChunks chunks is
// still sent.
func TestChunkLimitExact(t *testing.T) {
	_, exit := setupRoundTrip(t)
	cfg.Urls.Fetch = false
	cfg.Files.Pooldir = t.TempDir()
	flag.Chain = exit.address
	flag.Copies = 1
	chunkLimitMessage(t, maxChunks*maxFragLength)
	mixprep()
	if n := len(poolFiles(t, cfg.Files.Pooldir, "m")); n != maxChunks {
		t.Errorf("Pooled %d packets, want %d", n, maxChunks)
	}
}

// TestMessageReadLimit checks that the client stops reading an input
// larger than maxMessageRead instead of reading all of it.
func TestMessageReadLimit(t *testing.T) {
	if dir := os.Getenv("YAMN_CHILD"); dir != "" {
		_, exit := setupRoundTrip(t)
		cfg.Urls.Fetch = false
		cfg.Files.Pooldir = dir
		flag.Chain = exit.address
		flag.Copies = 1
		maxMessageRead = 1000
		chunkLimitMessage(t, 1001)
		mixprep()
		os.Exit(0)
	}
	dir := t.TempDir()
	out, err := runClientChild(t, "TestMessageReadLimit", dir)
	if err == nil || !strings.Contains(out, "Message larger than 1000 bytes") {
		t.Errorf("An input over maxMessageRead should fail: %v\n%s", err, out)
	}
	if n := len(poolFiles(t, dir, "m")); n != 0 {
		t.Errorf("%d packets pooled for a message that was refused", n)
	}
}
