package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMissingExplicitConfig checks that a config file given with --config
// or $YAMNCFG that doesn't exist is an error, not a silent fallback to
// ~/yamn.yml or the defaults.
func TestMissingExplicitConfig(t *testing.T) {
	_, home := isolateConfigLookup(t)
	writeConfigFile(t, filepath.Join(home, "yamn.yml"))
	missing := filepath.Join(t.TempDir(), "typo.yml")

	if c, err := (&Flags{Config: missing}).ParseConfig(); err == nil {
		t.Errorf("--config %s: no error, used %q", missing, c.Files.Config)
	}
	t.Setenv("YAMNCFG", missing)
	if c, err := (&Flags{}).ParseConfig(); err == nil {
		t.Errorf("YAMNCFG=%s: no error, used %q", missing, c.Files.Config)
	}
	// An empty YAMNCFG is the same as none.
	t.Setenv("YAMNCFG", "")
	if _, err := (&Flags{}).ParseConfig(); err != nil {
		t.Errorf("Empty YAMNCFG: %v", err)
	}
}

// TestUnknownOptions checks that unknown options give a warning each, and
// that files.config can't be set in the file.
func TestUnknownOptions(t *testing.T) {
	isolateConfigLookup(t)
	cfgFile := filepath.Join(t.TempDir(), "yamn.yml")
	cfgYAML := `
urls:
    pinger:
        - name: a
    key_qorum: 3
    key_quorum: 3
mail:
    requiretls: true
files:
    config: /somewhere/else.yml
`
	if err := os.WriteFile(cfgFile, []byte(cfgYAML), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := (&Flags{Config: cfgFile}).ParseConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.Files.Config != cfgFile {
		t.Errorf("files.config in the file changed the config file name to %q", c.Files.Config)
	}
	if c.Urls.KeyQuorum != 3 {
		t.Errorf("Known options should still be read: key_quorum %d", c.Urls.KeyQuorum)
	}
	warnings := strings.Join(c.Warnings, "\n")
	for _, want := range []string{
		`line 3: unknown option "pinger"`,
		`line 5: unknown option "key_qorum"`,
		`line 8: unknown option "requiretls"`,
		`line 10: unknown option "config"`,
	} {
		if !strings.Contains(warnings, want) {
			t.Errorf("No warning %q in:\n%s", want, warnings)
		}
	}
	if len(c.Warnings) != 4 {
		t.Errorf("Want 4 warnings, got %d:\n%s", len(c.Warnings), warnings)
	}

	// No warnings for a file with known options only.
	writeConfigFile(t, cfgFile)
	if c, err := (&Flags{Config: cfgFile}).ParseConfig(); err != nil || len(c.Warnings) != 0 {
		t.Errorf("Known options only: %v, warnings %q", err, c.Warnings)
	}
}

// TestValidate checks that values that would make yamn fail or panic
// later are errors when the config is read.
func TestValidate(t *testing.T) {
	isolateConfigLookup(t)
	for _, bad := range []string{
		"stats:\n    distance: -1\n",
		"stats:\n    minlat: -1\n",
		"stats:\n    minlat: 30\n    maxlat: 10\n",
		"stats:\n    minrel: 101\n",
		"stats:\n    minrel: -1\n",
		"stats:\n    rel_final: 100.5\n",
		"urls:\n    key_quorum: 0\n",
		"urls:\n    key_pinning: strict\n",
		"urls:\n    key_pinning: \"\"\n",
		"mail:\n    tor_socks: \"9050\"\n",
		"mail:\n    tor_socks: localhost\n",
		"mail:\n    tor_socks: \":9050\"\n",
		"mail:\n    tor_socks: 127.0.0.1:99999\n",
		"mail:\n    tor_socks: 127.0.0.1:tor\n",
	} {
		cfgFile := filepath.Join(t.TempDir(), "yamn.yml")
		if err := os.WriteFile(cfgFile, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := (&Flags{Config: cfgFile}).ParseConfig(); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	if err := (&Flags{}).newConfig().validate(); err != nil {
		t.Errorf("The defaults are invalid: %v", err)
	}
}
