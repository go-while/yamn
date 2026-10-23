package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The public key of go-echolot's signing test vectors.
const testPingerKey = "RWQgENJbJTXyiQw5YbtIXa2d+EMCmLkZ0ieF/LCB9BchV8xal3NYeg9J"

// TestPingerKeys checks the checks of pinger_keys, the pingers' keys and
// require_signatures.
func TestPingerKeys(t *testing.T) {
	isolateConfigLookup(t)
	parse := func(yml string) (*Config, error) {
		f := filepath.Join(t.TempDir(), "yamn.yml")
		if err := os.WriteFile(f, []byte(yml), 0600); err != nil {
			t.Fatal(err)
		}
		return (&Flags{Config: f}).ParseConfig()
	}
	for _, good := range []string{
		"urls:\n    pinger_keys: [" + testPingerKey + "]\n    require_signatures: true\n",
		"urls:\n    pinger_keys: [" + testPingerKey + "]\n    max_signature_age_hours: 0\n",
		"urls:\n    pingers:\n        - name: a\n          keys: [" + testPingerKey + "]\n" +
			"        - name: b\n          keys: [" + testPingerKey + "]\n    require_signatures: true\n",
		// Without require_signatures, a pinger may have no keys.
		"urls:\n    pingers:\n        - name: a\n          keys: [" + testPingerKey + "]\n        - name: b\n",
	} {
		c, err := parse(good)
		if err != nil {
			t.Errorf("%q: %v", good, err)
			continue
		}
		if len(c.Warnings) != 0 {
			t.Errorf("%q: warnings %q", good, c.Warnings)
		}
	}
	for _, bad := range []string{
		"urls:\n    pinger_keys: [notakey]\n",
		"urls:\n    pinger_keys: [" + testPingerKey[:20] + "]\n",
		"urls:\n    require_signatures: true\n",
		"urls:\n    max_signature_age_hours: -1\n",
		"urls:\n    pingers:\n        - name: a\n          keys: [notakey]\n",
		"urls:\n    pingers:\n        - name: a\n          keys: [" + testPingerKey + "]\n" +
			"        - name: b\n    require_signatures: true\n",
		"urls:\n    pinger_keys: [" + testPingerKey + "]\n    pingers:\n        - name: a\n",
	} {
		if _, err := parse(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	c := (&Flags{}).newConfig()
	if c.Urls.RequireSignatures || c.Urls.MaxSignatureAge != 48 || c.Files.Sigstate != "sigstate.json" {
		t.Errorf("defaults: require_signatures %v, max_signature_age_hours %d, sigstate %q",
			c.Urls.RequireSignatures, c.Urls.MaxSignatureAge, c.Files.Sigstate)
	}
}
