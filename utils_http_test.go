package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crooks/yamn/keymgr"
)

func TestHTTPGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte("hello"))
		case "/big":
			w.Write([]byte(strings.Repeat("x", maxDownloadBytes+1)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()

	okFile := filepath.Join(dir, "ok")
	if err := httpGet(srv.URL+"/ok", okFile, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if b, _ := os.ReadFile(okFile); string(b) != "hello" {
		t.Fatalf("expected file content \"hello\", got %q", b)
	}

	bigFile := filepath.Join(dir, "big")
	if err := httpGet(srv.URL+"/big", bigFile, nil); err == nil {
		t.Fatal("expected error for oversized download")
	}
	if _, err := os.Stat(bigFile); err == nil {
		t.Fatal("oversized download should not be written to disk")
	}

	if err := httpGet(srv.URL+"/missing", filepath.Join(dir, "missing"), nil); err == nil {
		t.Fatal("expected error for 404 response")
	}
}

// TestHTTPGetValidation checks that a download failing validation leaves the
// existing file untouched, and that a valid one replaces it without leaving
// temporary files behind.
func TestHTTPGetValidation(t *testing.T) {
	const goodPubring = "rem rem@example.invalid 00112233445566778899aabbccddeeff 4:0.2.7 E 2026-01-01 2100-01-01\n\n" +
		"-----Begin Mix Key-----\n00112233445566778899aabbccddeeff\n" + "aa" + "\n-----End Mix Key-----\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html":
			w.Write([]byte("<html><body>Service Unavailable</body></html>"))
		case "/pubring":
			w.Write([]byte(goodPubring))
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	file := filepath.Join(dir, "pubring.mix")
	if err := os.WriteFile(file, []byte("existing keyring"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := httpGet(srv.URL+"/html", file, keymgr.CheckPubring); err == nil {
		t.Fatal("An HTML page should be rejected as a pubring")
	}
	if b, _ := os.ReadFile(file); string(b) != "existing keyring" {
		t.Fatalf("Rejected download replaced the existing file: %q", b)
	}

	if err := httpGet(srv.URL+"/pubring", file, keymgr.CheckPubring); err != nil {
		t.Fatalf("Valid pubring rejected: %s", err)
	}
	if b, _ := os.ReadFile(file); string(b) != goodPubring {
		t.Fatalf("Valid download not written: %q", b)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("Expected only pubring.mix in %s, found %d entries", dir, len(entries))
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0644 {
		t.Errorf("File mode: got %o, want 644", info.Mode().Perm())
	}
}
