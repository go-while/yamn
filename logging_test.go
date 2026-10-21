package main

import (
	"bytes"
	stdlog "log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/log-go"
)

// TestLogFileMode checks that a new log file is only readable by its owner.
func TestLogFileMode(t *testing.T) {
	name := filepath.Join(t.TempDir(), "yamn.log")
	f, err := openLogFile(name)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0077 != 0 {
		t.Errorf("Log file created with mode %o, want 0600", mode)
	}
}

// TestClientChainNotLogged checks that the client logs its chain at debug
// level only, not at info.
func TestClientChainNotLogged(t *testing.T) {
	middle, exit := setupRoundTrip(t)
	cfg.Urls.Fetch = false
	cfg.Files.Pooldir = t.TempDir()
	flag.Client = true
	flag.Chain = middle.address + "," + exit.address
	flag.Copies = 1
	msgFile := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(msgFile, []byte("To: alice@example.invalid\n\nHello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	flag.Args = []string{msgFile}

	var buf bytes.Buffer
	oldLogger, oldOutput := log.Current, stdlog.Writer()
	t.Cleanup(func() { log.Current = oldLogger; stdlog.SetOutput(oldOutput) })
	stdlog.SetOutput(&buf)
	log.Current = log.StdLogger{Level: log.InfoLevel}
	mixprep()
	if strings.Contains(buf.String(), middle.address) {
		t.Errorf("The chain is logged at info level:\n%s", buf.String())
	}

	buf.Reset()
	log.Current = log.StdLogger{Level: log.DebugLevel}
	mixprep()
	if !strings.Contains(buf.String(), "Chain: "+middle.address) {
		t.Errorf("The chain should be logged at debug level:\n%s", buf.String())
	}
}
