package quickmail

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHeaders(t *testing.T) {
	m := NewMessage()
	m.Set("Subject", "Hello")
	if m.Get("Subject") != "Hello" {
		t.Fatalf("Expected Subject \"Hello\", got %q", m.Get("Subject"))
	}
	m.Del("Subject")
	if m.Get("Subject") != "" {
		t.Fatalf("Subject should be empty after Del, got %q", m.Get("Subject"))
	}
}

func TestCompileRequiresFromTo(t *testing.T) {
	m := NewMessage()
	if _, err := m.Compile(); err == nil {
		t.Error("Compile without From and To should fail")
	}
	m.Set("From", "remailer@example.invalid")
	if _, err := m.Compile(); err == nil {
		t.Error("Compile without To should fail")
	}
	m.Del("From")
	m.Set("To", "user@example.invalid")
	if _, err := m.Compile(); err == nil {
		t.Error("Compile without From should fail")
	}
}

func TestCompile(t *testing.T) {
	bodyFile := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(bodyFile, []byte("File content"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewMessage()
	m.Set("From", "remailer@example.invalid")
	m.Set("To", "user@example.invalid")
	m.Set("Subject", "Test")
	m.Text("Prefix ")
	m.Text("text")
	m.Filename = bodyFile
	m.List([]string{"item1", "item2"})
	m.Suffix = "Suffix"

	b, err := m.Compile()
	if err != nil {
		t.Fatalf("Compile failed: %s", err)
	}
	headers, body, found := strings.Cut(string(b), "\n\n")
	if !found {
		t.Fatalf("No blank line between headers and body:\n%s", b)
	}
	// Header order follows map iteration, so compare them sorted.
	gotHeaders := strings.Split(headers, "\n")
	slices.Sort(gotHeaders)
	wantHeaders := []string{
		"From: remailer@example.invalid",
		"Subject: Test",
		"To: user@example.invalid",
	}
	if !slices.Equal(gotHeaders, wantHeaders) {
		t.Errorf("Headers: expected %q, got %q", wantHeaders, gotHeaders)
	}
	wantBody := "Prefix text\nFile content\nitem1\nitem2\nSuffix\n"
	if body != wantBody {
		t.Errorf("Body: expected %q, got %q", wantBody, body)
	}
}

func TestCompileMissingFile(t *testing.T) {
	m := NewMessage()
	m.Set("From", "remailer@example.invalid")
	m.Set("To", "user@example.invalid")
	m.Filename = filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := m.Compile(); err == nil {
		t.Fatal("Compile with a missing file should fail")
	}
}
