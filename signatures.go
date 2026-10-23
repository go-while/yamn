package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Masterminds/log-go"
	"github.com/crooks/yamn/config"
	"github.com/crooks/yamn/minisign"
)

// Pinger signatures (Y-03): a pinger with a minisign key publishes
// <file>.minisig next to pubring.mix and mlist2.txt.  When keys are
// configured for a download, it's only used if the signature verifies
// with one of them, names the file, hasn't expired, isn't from the future
// or older than urls.max_signature_age_hours, and isn't older than the
// newest signature accepted before by that key for that file.

const (
	signatureSuffix = ".minisig"
	// maxSignatureSkew is how far a signature's timestamp may be ahead of
	// the clock.  A pinger signs at most 15 minutes ahead of its own clock.
	maxSignatureSkew = 30 * time.Minute
)

// pingerKeys parses configured keys.  The config check has already
// rejected bad ones.
func pingerKeys(keys []string) []minisign.PublicKey {
	pubs, err := config.PublicKeys(keys)
	if err != nil {
		log.Warn(err)
	}
	return pubs
}

// fetchFile downloads url into filename, as httpGet does.  With keys, the
// download must also carry a valid signature by one of them.
func fetchFile(url, filename string, validate func([]byte) error, keys []minisign.PublicKey) error {
	if len(keys) == 0 {
		return httpGet(url, filename, validate)
	}
	var accept func() error
	check := func(content []byte) error {
		a, err := checkSignature(url, content, keys, cfg.Files.Sigstate, time.Now())
		if err != nil {
			return err
		}
		if validate != nil {
			if err := validate(content); err != nil {
				return err
			}
		}
		accept = a
		return nil
	}
	if err := httpGet(url, filename, check); err != nil {
		return err
	}
	return accept()
}

// signatureURL returns the URL of rawURL's signature, and the file name the
// signature must name: the last element of its path.
func signatureURL(rawURL string) (sigURL, name string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	name = path.Base(u.Path)
	if name == "/" || name == "." {
		return "", "", fmt.Errorf("%s: no file name to check the signature against", rawURL)
	}
	u.Path += signatureSuffix
	u.RawPath = ""
	return u.String(), name, nil
}

// checkSignature downloads and checks the signature of content, fetched
// from url.  It returns a function that records the signature as the
// newest accepted, to be called once the file is in place.
func checkSignature(url string, content []byte, keys []minisign.PublicKey, stateFile string, now time.Time) (accept func() error, err error) {
	sigURL, name, err := signatureURL(url)
	if err != nil {
		return
	}
	sigData, err := download(sigURL, minisign.MaxSignatureFile)
	if err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}
	sig, err := minisign.ParseSignature(sigData)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", sigURL, err)
	}
	key, err := sig.Verify(keys, content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", sigURL, err)
	}
	t, err := minisign.ParseTrusted(sig.Trusted)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", sigURL, err)
	}
	when := func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") }
	switch maxAge := time.Duration(cfg.Urls.MaxSignatureAge) * time.Hour; {
	case t.File != name:
		return nil, fmt.Errorf("%s: signature is for %q, not %q", sigURL, t.File, name)
	case !now.Before(t.Expires):
		return nil, fmt.Errorf("%s: signature expired at %s", sigURL, when(t.Expires))
	case t.Timestamp.After(now.Add(maxSignatureSkew)):
		return nil, fmt.Errorf("%s: signature made in the future, at %s", sigURL, when(t.Timestamp))
	case maxAge > 0 && now.Sub(t.Timestamp) > maxAge:
		return nil, fmt.Errorf("%s: signature made at %s, more than %d hours ago",
			sigURL, when(t.Timestamp), cfg.Urls.MaxSignatureAge)
	}
	id := sigStateID(key.ID, t.File)
	state, err := loadSigState(stateFile)
	if err != nil {
		return
	}
	if last, ok := state[id]; ok && t.Timestamp.Unix() < last {
		return nil, fmt.Errorf("%s: signature made at %s, older than the one accepted before (%s)",
			sigURL, when(t.Timestamp), when(time.Unix(last, 0)))
	}
	accept = func() error {
		log.Debugf("%s: signed by %s at %s", url, key.ID, when(t.Timestamp))
		// Read the state again: another yamn process may have written it.
		state, err := loadSigState(stateFile)
		if err != nil {
			return err
		}
		if t.Timestamp.Unix() <= state[id] {
			return nil
		}
		state[id] = t.Timestamp.Unix()
		return state.save(stateFile)
	}
	return accept, nil
}

// sigState holds the newest timestamp accepted per pinger key and file,
// in Unix seconds.  It's per key, not per pinger, so a pinger's new key
// starts afresh (go-echolot's way out of a clock that was far ahead).
type sigState map[string]int64

func sigStateID(id minisign.KeyID, file string) string {
	return id.String() + " " + file
}

// loadSigState reads the state file.  A missing file is an empty state; a
// damaged one is an error, since ignoring it would accept older files.
func loadSigState(file string) (sigState, error) {
	state := make(sigState)
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("%s is damaged (%v); remove it to accept signatures again", file, err)
	}
	return state, nil
}

func (s sigState) save(file string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(file, append(data, '\n'), 0600)
}

// plainHTTP reports whether url is fetched without TLS.
func plainHTTP(url string) bool {
	return strings.HasPrefix(strings.ToLower(url), "http://")
}
