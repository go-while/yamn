package main

import (
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// clientExitChild runs the test named name again in a child process, with
// YAMN_EXIT_CHILD set to a new pool directory, for client code that ends
// with os.Exit.  It returns the output, the number of packets pooled and
// the error from the child (nil for exit status 0).
func clientExitChild(t *testing.T, name string) (string, int, error) {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	cmd.Env = append(os.Environ(), "YAMN_EXIT_CHILD="+dir)
	out, err := cmd.CombinedOutput()
	return string(out), len(poolFiles(t, dir, "m")), err
}

// clientExitMessage makes msg the client's input file.
func clientExitMessage(t *testing.T, msg string) {
	t.Helper()
	msgFile := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(msgFile, []byte(msg), 0600); err != nil {
		t.Fatal(err)
	}
	flag.Args = []string{msgFile}
}

// TestClientChainErrorExitStatus checks that a chain that can't be built
// (an unknown remailer) ends the client with exit status 1, not 0.
func TestClientChainErrorExitStatus(t *testing.T) {
	if dir := os.Getenv("YAMN_EXIT_CHILD"); dir != "" {
		setupRandomExits(t)
		cfg.Urls.Fetch = false
		cfg.Files.Pooldir = dir
		flag.Chain = "nosuchremailer,*"
		flag.Copies = 1
		clientExitMessage(t, "To: alice@example.invalid\n\nHello\n")
		mixprep()
		os.Exit(0)
	}
	out, pooled, err := clientExitChild(t, "TestClientChainErrorExitStatus")
	if err == nil || pooled != 0 {
		t.Errorf("Unknown remailer: want exit status 1 and nothing pooled, got %v, %d pooled\n%s", err, pooled, out)
	}
}

// TestClientImportFailureExitStatus checks that a missing keyring ends the
// client with exit status 1.
func TestClientImportFailureExitStatus(t *testing.T) {
	if dir := os.Getenv("YAMN_EXIT_CHILD"); dir != "" {
		setupTestConfig(t)
		cfg.Urls.Fetch = false
		cfg.Files.Pooldir = dir
		cfg.Files.Pubring = filepath.Join(dir, "missing.mix")
		clientExitMessage(t, "To: alice@example.invalid\n\nHello\n")
		mixprep()
		os.Exit(0)
	}
	out, _, err := clientExitChild(t, "TestClientImportFailureExitStatus")
	if err == nil {
		t.Errorf("Missing pubring: want exit status 1, got 0\n%s", out)
	}
}

// TestClientDummyFailureExitStatus checks that a dummy that can't be built
// after the message was pooled is only a warning: the client exits 0, so
// the user doesn't send the message again.
func TestClientDummyFailureExitStatus(t *testing.T) {
	if dir := os.Getenv("YAMN_EXIT_CHILD"); dir != "" {
		middle, exits := setupRandomExits(t)
		if err := Pubring.ImportStats(); err != nil {
			t.Fatal(err)
		}
		cfg.Urls.Fetch = false
		cfg.Files.Pooldir = dir
		cfg.Stats.Relfinal = 100.1 // No random exit qualifies, for the dummy
		cfg.Stats.Chain = middle.name + "," + exits[0].name
		flag.Copies = 1
		clientExitMessage(t, "To: alice@example.invalid\n\nHello\n")
		mixprep()
		dummy() // What mixprep does with probability 80/256
		os.Exit(0)
	}
	out, pooled, err := clientExitChild(t, "TestClientDummyFailureExitStatus")
	if err != nil || pooled != 1 {
		t.Errorf("Failed dummy: want exit status 0 and the message pooled, got %v, %d pooled\n%s", err, pooled, out)
	}
}

// TestClientNoRecipient checks that a message without a To, Cc or Bcc
// address is refused instead of being sent and dropped at the exit.
func TestClientNoRecipient(t *testing.T) {
	if dir := os.Getenv("YAMN_EXIT_CHILD"); dir != "" {
		_, exit := setupRoundTrip(t)
		cfg.Urls.Fetch = false
		cfg.Files.Pooldir = dir
		flag.Chain = exit.address
		flag.Copies = 1
		clientExitMessage(t, "Subject: hi\nTo: not an address\n\nno recipient\n")
		mixprep()
		os.Exit(0)
	}
	out, pooled, err := clientExitChild(t, "TestClientNoRecipient")
	if err == nil || pooled != 0 || !strings.Contains(out, "No recipient") {
		t.Errorf("No recipient: want exit status 1 and nothing pooled, got %v, %d pooled\n%s", err, pooled, out)
	}
}

func TestHasRecipient(t *testing.T) {
	for _, tt := range []struct {
		head string
		want bool
	}{
		{"Subject: x\n", false},
		{"To: bob\n", false},
		{"To: bob@example.invalid\n", true},
		{"Cc: Bob <bob@example.invalid>\n", true},
		{"Bcc: bob@example.invalid\n", true},
	} {
		msg, err := mail.ReadMessage(strings.NewReader(tt.head + "\nbody\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got := hasRecipient(msg.Header); got != tt.want {
			t.Errorf("%q: hasRecipient = %v, want %v", tt.head, got, tt.want)
		}
	}
}

// TestClientTooManyArgs checks that a third argument is an error instead
// of being ignored.
func TestClientTooManyArgs(t *testing.T) {
	if dir := os.Getenv("YAMN_EXIT_CHILD"); dir != "" {
		_, exit := setupRoundTrip(t)
		cfg.Urls.Fetch = false
		cfg.Files.Pooldir = dir
		flag.Chain = exit.address
		flag.Copies = 1
		clientExitMessage(t, "To: alice@example.invalid\n\nHello\n")
		flag.Args = []string{"bob@example.invalid", flag.Args[0], "extra"}
		mixprep()
		os.Exit(0)
	}
	out, pooled, err := clientExitChild(t, "TestClientTooManyArgs")
	if err == nil || pooled != 0 || !strings.Contains(out, "Too many arguments") {
		t.Errorf("Three arguments: want exit status 1 and nothing pooled, got %v, %d pooled\n%s", err, pooled, out)
	}
}

func TestParseChain(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want []string // nil: an error
	}{
		{"*,*,*", []string{"*", "*", "*"}},
		{"*, *", []string{"*", "*"}},
		{" alpha , *", []string{"alpha", "*"}},
		{"*,,*", nil},
		{"", nil},
		{" ", nil},
		{"*,", nil},
	} {
		got, err := parseChain(tt.in)
		if (err != nil) != (tt.want == nil) || !slices.Equal(got, tt.want) {
			t.Errorf("parseChain(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

// TestClientChainWithSpaces sends a message with --chain "*, exit".
func TestClientChainWithSpaces(t *testing.T) {
	_, exits := setupRandomExits(t)
	cfg.Urls.Fetch = false
	cfg.Files.Pooldir = t.TempDir()
	flag.Chain = "*, " + exits[0].name
	flag.Copies = 1
	clientExitMessage(t, "To: alice@example.invalid\n\nHello\n")
	mixprep()
	if n := len(poolFiles(t, cfg.Files.Pooldir, "m")); n != 1 {
		t.Errorf("Chain with a space: %d packets pooled, want 1", n)
	}
}
