package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHomePaths checks that a leading "~/" in a path of the files section
// is the home directory, with and without --dir.
func TestHomePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgFile := filepath.Join(t.TempDir(), "yamn.yml")
	cfgYAML := "files:\n    pubring: ~/yamn/pubring.mix\n    pooldir: \"~\"\n    mlist2: ~x/mlist2.txt\n"
	if err := os.WriteFile(cfgFile, []byte(cfgYAML), 0600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"", "/fakedir"} {
		c, err := (&Flags{Config: cfgFile, Dir: dir}).ParseConfig()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, "yamn/pubring.mix"); c.Files.Pubring != want {
			t.Errorf("--dir %q: pubring = %q, want %q", dir, c.Files.Pubring, want)
		}
		if c.Files.Pooldir != home {
			t.Errorf("--dir %q: pooldir = %q, want %q", dir, c.Files.Pooldir, home)
		}
		// "~user" isn't expanded: it's a relative path like any other.
		if m := c.Files.Mlist2; !strings.HasSuffix(m, "~x/mlist2.txt") ||
			(dir != "" && m != filepath.Join(dir, "~x/mlist2.txt")) {
			t.Errorf("--dir %q: mlist2 = %q, want ~x/mlist2.txt unexpanded", dir, m)
		}
	}
}
