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

// TestHTTPGetDowngradeRedirect checks that an https download isn't
// redirected to plain http, while https to https still works.
func TestHTTPGetDowngradeRedirect(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("served over plain http\n"))
	}))
	defer plain.Close()
	var secure *httptest.Server
	secure = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/downgrade":
			http.Redirect(w, r, plain.URL+"/pubring.mix", http.StatusFound)
		case "/moved":
			http.Redirect(w, r, secure.URL+"/pubring.mix", http.StatusFound)
		default:
			w.Write([]byte("served over https\n"))
		}
	}))
	defer secure.Close()
	// Trust the test server's certificate.
	old := http.DefaultTransport
	http.DefaultTransport = secure.Client().Transport
	defer func() { http.DefaultTransport = old }()
	out := filepath.Join(t.TempDir(), "pubring.mix")
	if err := httpGet(secure.URL+"/downgrade", out, nil); err == nil {
		t.Errorf("https URL was redirected to %s and the plain http download was accepted", plain.URL)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("the plain http download was written")
	}
	if err := httpGet(secure.URL+"/moved", out, nil); err != nil {
		t.Errorf("https to https redirect: %s", err)
	}
	if b, _ := os.ReadFile(out); string(b) != "served over https\n" {
		t.Errorf("got %q", b)
	}
}

// TestHTTPGetValidation checks that a download failing validation leaves the
// existing file untouched, and that a valid one replaces it without leaving
// temporary files behind.
func TestHTTPGetValidation(t *testing.T) {
	_, key := newTestRemailer(t, t.TempDir(), "rem", true)
	goodPubring := string(key)
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
