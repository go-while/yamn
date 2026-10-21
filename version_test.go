package main

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crooks/yamn/keymgr"
	"golang.org/x/crypto/nacl/box"
)

// TestVersionFormat checks the fork's version: the upstream version plus
// "+gw<N>".
func TestVersionFormat(t *testing.T) {
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(upstreamVersion) {
		t.Errorf("upstreamVersion %q is not an upstream release number", upstreamVersion)
	}
	if !regexp.MustCompile(`^` + regexp.QuoteMeta(upstreamVersion) + `\+gw[0-9]+$`).MatchString(version) {
		t.Errorf("version %q is not %s+gw<N>", version, upstreamVersion)
	}
}

// TestMailsCarryUpstreamVersion checks that encrypted mails carry only the
// upstream version, so they look like mails from upstream yamn.
func TestMailsCarryUpstreamVersion(t *testing.T) {
	setupTestConfig(t)
	var b bytes.Buffer
	if err := writeYamnMail(&b, "rem@example.org", make([]byte, messageBytes)); err != nil {
		t.Fatal(err)
	}
	mail := b.String()
	for _, want := range []string{
		"Subject: yamn-" + upstreamVersion + "\n",
		"Remailer-Type: yamn-" + upstreamVersion + "\n",
	} {
		if !strings.Contains(mail, want) {
			t.Errorf("mail lacks %q", want)
		}
	}
	if strings.Contains(mail, "+gw") {
		t.Errorf("mail reveals the fork version:\n%s", mail[:200])
	}
}

// TestKeyHeaderCarriesForkVersion checks that a remailer's published key
// shows the fork version, and that yamn still reads such a key.
func TestKeyHeaderCarriesForkVersion(t *testing.T) {
	dir := t.TempDir()
	pubFile := filepath.Join(dir, "key.txt")
	s := keymgr.NewSecring(filepath.Join(dir, "secring.mix"), pubFile)
	s.SetName("test")
	s.SetAddress("test@example.org")
	s.SetExit(true)
	s.SetValidity(14, 28)
	s.SetVersion(version)
	pub, sec, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s.WritePublic(pub[:], s.Insert(pub[:], sec[:]))
	key, err := os.ReadFile(pubFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(key, []byte(" 4:"+version+" ")) {
		t.Errorf("key header lacks 4:%s:\n%s", version, key)
	}
	p := keymgr.NewPubring(pubFile, filepath.Join(dir, "mlist2.txt"))
	if err := p.ImportPubring(); err != nil {
		t.Fatal(err)
	}
	if p.Count() != 1 {
		t.Errorf("a key with version %s doesn't import", version)
	}
}
