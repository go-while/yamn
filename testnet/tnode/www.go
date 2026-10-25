package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const statsEvery = 30 * time.Minute

func wwwMain(args []string) {
	fs := flag.NewFlagSet("www", flag.ExitOnError)
	listen := fs.String("listen", ":8080", "address to listen on")
	serve := mapFlag{}
	fs.Var(serve, "serve", "url path=directory, repeatable (e.g. /=www, /pinger/=results)")
	mlist2 := fs.String("mlist2", "", "directory whose pubring.mix gets a perfect mlist2.txt next to it, every 30 minutes")
	fs.Parse(args)
	if len(serve) == 0 {
		log.Fatal("nothing to serve: give -serve path=directory")
	}
	mux := http.NewServeMux()
	for path, dir := range serve {
		if !strings.HasPrefix(path, "/") || !strings.HasSuffix(path, "/") {
			log.Fatalf("-serve %s: the path must start and end with /", path)
		}
		mux.Handle(path, http.StripPrefix(path, http.FileServer(http.Dir(dir))))
		log.Printf("serving %s at %s", dir, path)
	}
	if *mlist2 != "" {
		go func() {
			for {
				if err := writeMlist2(*mlist2, time.Now()); err != nil {
					log.Printf("mlist2: %v", err)
				}
				time.Sleep(statsEvery)
			}
		}()
	}
	srv := &http.Server{Addr: *listen, Handler: logRequests(mux), ReadHeaderTimeout: 30 * time.Second}
	log.Printf("www listening on %s", *listen)
	log.Fatal(srv.ListenAndServe())
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("GET %s from %s", r.URL.Path, r.RemoteAddr)
		h.ServeHTTP(w, r)
	})
}

// pubringNames returns the remailer names in a pubring.mix: the first
// field of each key header (name address keyid version caps from until).
func pubringNames(data []byte) []string {
	var names []string
	seen := make(map[string]bool)
	s := bufio.NewScanner(bytes.NewReader(data))
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) == 7 && strings.Contains(f[1], "@") && !seen[f[0]] {
			seen[f[0]] = true
			names = append(names, f[0])
		}
	}
	sort.Strings(names)
	return names
}

// mlist2For returns an mlist2.txt that lists every name as perfect, with
// a latency of 5 minutes: inside yamn's default stats.minlat (2) and
// stats.maxlat (60), so every remailer can be picked for a chain.
func mlist2For(names []string, now time.Time) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Stats-Version: 2.0\nGenerated: %s\n", now.UTC().Format("Mon 02 Jan 2006 15:04:05 GMT"))
	b.WriteString("Mixmaster    Latent-Hist   Latent  Uptime-Hist   Uptime  Options\n")
	b.WriteString("------------------------------------------------------------------------\n")
	for _, n := range names {
		fmt.Fprintf(&b, "%-12s %s  %6s   %s  %5.1f%%\n", n, "000000000000", ":05", "++++++++++++", 100.0)
	}
	b.WriteString("\n")
	return b.Bytes()
}

// writeMlist2 writes dir/mlist2.txt for the remailers in dir/pubring.mix.
func writeMlist2(dir string, now time.Time) error {
	data, err := os.ReadFile(filepath.Join(dir, "pubring.mix"))
	if err != nil {
		return err
	}
	names := pubringNames(data)
	if len(names) == 0 {
		return fmt.Errorf("no remailers in %s", filepath.Join(dir, "pubring.mix"))
	}
	if err := writeAtomic(filepath.Join(dir, "mlist2.txt"), mlist2For(names, now)); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(dir, "mlist2.txt"), 0644)
}
