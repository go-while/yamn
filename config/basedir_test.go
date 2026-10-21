package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestBaseDir checks where relative paths of the files section are read
// from without --dir: the directory of a config file given with --config
// or $YAMNCFG, and otherwise the current directory, with a warning.
func TestBaseDir(t *testing.T) {
	cwd, home := isolateConfigLookup(t)
	cfgDir := t.TempDir()
	cfgFile := writeConfigFile(t, filepath.Join(cfgDir, "yamn.yml"))
	homeCfg := filepath.Join(home, "yamn.yml")
	dir := t.TempDir()

	check := func(name string, f *Flags, wantDir string, wantWarning string) {
		t.Helper()
		c, err := f.ParseConfig()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := filepath.Join(wantDir, "pubring.mix"); c.Files.Pubring != want {
			t.Errorf("%s: pubring %q, want %q", name, c.Files.Pubring, want)
		}
		warnings := strings.Join(c.PathWarnings(), "\n")
		if (wantWarning == "") != (warnings == "") || !strings.Contains(warnings, wantWarning) {
			t.Errorf("%s: warnings %q, want %q", name, warnings, wantWarning)
		}
	}
	check("--dir", &Flags{Dir: dir, Config: cfgFile}, dir, "")
	check("--config", &Flags{Config: cfgFile}, cfgDir, "")
	t.Setenv("YAMNCFG", cfgFile)
	check("$YAMNCFG", &Flags{}, cfgDir, "")
	t.Setenv("YAMNCFG", "")
	// Found in a shared place: the current directory as before, but with
	// a warning.
	writeConfigFile(t, homeCfg)
	check("~/yamn.yml", &Flags{}, "", "No --dir given")
	os.Remove(homeCfg)
	check("no config file", &Flags{}, "", "No --dir given")

	// The current directory holds yamn's files and the config file's
	// directory doesn't: the current directory is still used.
	if err := os.WriteFile(filepath.Join(cwd, "pubring.mix"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	check("--config, files in the current directory", &Flags{Config: cfgFile}, "", "not from the config file's directory")
	// Once the config file's directory has them, it's used.
	if err := os.WriteFile(filepath.Join(cfgDir, "pubring.mix"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	check("--config, files in both", &Flags{Config: cfgFile}, cfgDir, "")
}

// TestBaseDirAbsolutePaths checks that there's no warning when all paths
// are absolute.
func TestBaseDirAbsolutePaths(t *testing.T) {
	_, home := isolateConfigLookup(t)
	c := (&Flags{}).newConfig()
	v := reflect.ValueOf(&c.Files).Elem()
	var yml strings.Builder
	yml.WriteString("files:\n")
	for i := 0; i < v.NumField(); i++ {
		if tag := v.Type().Field(i).Tag.Get("yaml"); tag != "" && tag != "-" {
			yml.WriteString("    " + tag + ": /abs/" + tag + "\n")
		}
	}
	if err := os.WriteFile(filepath.Join(home, "yamn.yml"), []byte(yml.String()), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := (&Flags{}).ParseConfig()
	if err != nil {
		t.Fatal(err)
	}
	if w := c.PathWarnings(); len(w) != 0 {
		t.Errorf("Only absolute paths, but warnings: %q", w)
	}
}

// TestRelativePathsCoversFiles checks that relativePaths looks at every
// path of the files section.
func TestRelativePathsCoversFiles(t *testing.T) {
	c := new(Config)
	v := reflect.ValueOf(&c.Files).Elem()
	n := 0
	for i := 0; i < v.NumField(); i++ {
		if tag := v.Type().Field(i).Tag.Get("yaml"); tag != "" && tag != "-" {
			v.Field(i).SetString("rel")
			n++
		}
	}
	if got := len(c.relativePaths()); got != n {
		t.Errorf("relativePaths found %d of %d relative paths", got, n)
	}
}
