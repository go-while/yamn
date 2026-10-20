package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err := httpGet(srv.URL+"/ok", okFile); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if b, _ := os.ReadFile(okFile); string(b) != "hello" {
		t.Fatalf("expected file content \"hello\", got %q", b)
	}

	bigFile := filepath.Join(dir, "big")
	if err := httpGet(srv.URL+"/big", bigFile); err == nil {
		t.Fatal("expected error for oversized download")
	}
	if _, err := os.Stat(bigFile); err == nil {
		t.Fatal("oversized download should not be written to disk")
	}

	if err := httpGet(srv.URL+"/missing", filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected error for 404 response")
	}
}
