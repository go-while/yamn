package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeSendmail writes a script that records its arguments (one per line)
// and its input, and returns the script and the two files.
func fakeSendmail(t *testing.T) (script, argsFile, inFile string) {
	t.Helper()
	dir := t.TempDir()
	script = filepath.Join(dir, "sendmail")
	argsFile = filepath.Join(dir, "args")
	inFile = filepath.Join(dir, "in")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\ncat > " + inFile + "\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return
}

// TestMailPoolFilePipeRecipients checks that pipe mode passes the
// recipients left after the block list, Bcc included, as arguments after
// "--".  The command used to get no recipients, so with "sendmail -t" the
// block list was ignored and Bcc recipients got nothing.
func TestMailPoolFilePipeRecipients(t *testing.T) {
	setupTestConfig(t)
	cfg.Files.Pooldir = t.TempDir()
	cfg.Files.DestBlock = filepath.Join(t.TempDir(), "dest.blk")
	if err := os.WriteFile(cfg.Files.DestBlock, []byte("alice@example.invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, opts := range []string{" -i", " -i --"} {
		script, argsFile, inFile := fakeSendmail(t)
		cfg.Mail.Pipe = script + opts
		file := writePoolMessage(t, "To: alice@example.invalid, carol@example.invalid\nBcc: dave@example.invalid\n")
		if _, err := mailPoolFile(file); err != nil {
			t.Fatalf("mailPoolFile failed: %s", err)
		}
		args, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Fields(string(args))
		want := []string{"-i", "--", "carol@example.invalid", "dave@example.invalid"}
		if !slices.Equal(got, want) {
			t.Errorf("pipe %q: arguments %q, want %q", cfg.Mail.Pipe, got, want)
		}
		in, err := os.ReadFile(inFile)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(in), "dave@") {
			t.Errorf("Bcc recipient visible in the piped message: %q", in)
		}
	}
}

func TestPipeReadsHeaders(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/usr/sbin/sendmail -t":         true,
		"/usr/sbin/sendmail -i -t":      true,
		"/usr/sbin/sendmail -ti":        true,
		"/usr/sbin/sendmail -it":        true,
		"/usr/sbin/sendmail -i":         false,
		"/usr/sbin/sendmail -oi":        false,
		"/usr/sbin/sendmail -i --":      false,
		"/usr/sbin/sendmail -i -- -t":   false,
		"/usr/sbin/sendmail -f me@x -i": false,
		"/opt/t/-t":                     false,
		"":                              false,
	} {
		if got := pipeReadsHeaders(cmd); got != want {
			t.Errorf("pipeReadsHeaders(%q) = %v, want %v", cmd, got, want)
		}
	}
}
