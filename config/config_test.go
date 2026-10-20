package config

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestFlags(t *testing.T) {
	expectedClient := false
	expectedCopies := 0 // Not given: the config's num_copies applies
	// ParseFlags registers on the global FlagSet and parses os.Args.  Give
	// it a fresh FlagSet and no arguments so the test can run repeatedly.
	oldCommandLine, oldArgs := flag.CommandLine, os.Args
	defer func() { flag.CommandLine, os.Args = oldCommandLine, oldArgs }()
	flag.CommandLine = flag.NewFlagSet("yamn", flag.ContinueOnError)
	os.Args = []string{"yamn"}
	f := ParseFlags()
	if f.Client != expectedClient {
		t.Errorf("Expected Client to contain \"%v\" but got \"%v\".", expectedClient, f.Client)
	}
	if f.Copies != expectedCopies {
		t.Errorf("Expected Copies to contain \"%v\" but got \"%v\".", expectedCopies, f.Copies)
	}
}

func TestConfig(t *testing.T) {
	testFile, err := os.CreateTemp(t.TempDir(), "yamn")
	if err != nil {
		t.Fatalf("Unable to create TempFile: %v", err)
	}
	fakeCfg := `---
general:
  logtofile: true
  loglevel: info
files:
  pubkey: /fake/dir/pubkey
`
	if _, err := testFile.WriteString(fakeCfg); err != nil {
		t.Fatalf("Unable to write TempFile: %v", err)
	}
	if err := testFile.Close(); err != nil {
		t.Fatalf("Unable to close TempFile: %v", err)
	}
	f := new(Flags)
	// Populate the Config flag with the new testFile name.  This will override any other yamn config files that might
	// be lurking.
	f.Config = testFile.Name()
	// act as if we've been called with --dir=/fakedir
	f.Dir = "/fakedir"
	c, err := f.ParseConfig()
	if err != nil {
		t.Fatalf("ParseConfig returned: %v", err)
	}
	// c.Files.Config is special, it returns the name of the processed YAML file
	if c.Files.Config != testFile.Name() {
		t.Errorf("expected c.Files.Config to contain \"%s\" but got \"%s\"", testFile.Name(), c.Files.Config)
	}
	// These settings are defined in the fake config
	if !c.General.LogToFile {
		t.Errorf("expected c.General.LogToFile to be true but got %v", c.General.LogToFile)
	}
	if c.General.Loglevel != "info" {
		t.Errorf("expected c.General.Loglevel to contain \"info\" but got \"%s\"", c.General.Loglevel)
	}
	if c.Files.Pubkey != "/fake/dir/pubkey" {
		t.Errorf("expected c.Files.Pubkey to contain \"/fake/dir/pubkey\" but got \"%s\"", c.Files.Pubkey)
	}
	// These settings are undefined and should return defaults
	if !c.Urls.Fetch {
		t.Errorf("expected c.Urls.Fetch to default to true but got %v", c.Urls.Fetch)
	}
	if !c.Mail.UseTLS {
		t.Errorf("expected c.Mail.UseTLS to default to true but got %v", c.Mail.UseTLS)
	}
	// These settings inherit defaults from flags
	if c.Files.IDlog != "/fakedir/idlog" {
		t.Errorf("Expected c.Files.IDlog to default to \"/fakedir/idlog\" but got \"%s\".", c.Files.IDlog)
	}
}

// TestEmptyConfig checks that a config file holding an empty YAML document
// keeps the defaults instead of returning a nil Config.
func TestEmptyConfig(t *testing.T) {
	for _, content := range []string{"", "---\n", "# comment only\n"} {
		cfgFile := filepath.Join(t.TempDir(), "yamn.yml")
		if err := os.WriteFile(cfgFile, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		f := &Flags{Config: cfgFile, Dir: "/fakedir"}
		c, err := f.ParseConfig()
		if err != nil {
			t.Fatalf("ParseConfig(%q) returned: %v", content, err)
		}
		if c == nil {
			t.Fatalf("ParseConfig(%q) returned a nil Config", content)
		}
		if c.General.Loglevel != "warn" {
			t.Errorf("ParseConfig(%q): expected default loglevel \"warn\", got %q", content, c.General.Loglevel)
		}
	}
}

// isolateConfigLookup runs the test in an empty working directory with an
// empty home directory, no YAMNCFG and no system config.  It returns the
// working and home directories.
func isolateConfigLookup(t *testing.T) (cwd, home string) {
	t.Helper()
	cwd, home = t.TempDir(), t.TempDir()
	t.Chdir(cwd)
	t.Setenv("HOME", home)
	t.Setenv("YAMNCFG", "")
	oldSystem := systemConfig
	systemConfig = filepath.Join(t.TempDir(), "yamn.yml")
	t.Cleanup(func() { systemConfig = oldSystem })
	return
}

func writeConfigFile(t *testing.T, name string) string {
	t.Helper()
	if err := os.WriteFile(name, []byte("---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

// TestFindConfigIgnoresWorkingDir checks that a yamn.yml in the current
// directory is not picked up, with or without an empty --dir.
func TestFindConfigIgnoresWorkingDir(t *testing.T) {
	cwd, _ := isolateConfigLookup(t)
	writeConfigFile(t, filepath.Join(cwd, "yamn.yml"))
	f := &Flags{}
	if got, err := f.findConfig(); err == nil {
		t.Fatalf("findConfig should not use ./yamn.yml, got %s", got)
	}
}

func TestFindConfigOrder(t *testing.T) {
	cwd, home := isolateConfigLookup(t)
	writeConfigFile(t, filepath.Join(cwd, "yamn.yml"))
	dir := t.TempDir()
	flagCfg := writeConfigFile(t, filepath.Join(t.TempDir(), "flag.yml"))
	envCfg := writeConfigFile(t, filepath.Join(t.TempDir(), "env.yml"))
	dirCfg := writeConfigFile(t, filepath.Join(dir, "yamn.yml"))
	homeCfg := writeConfigFile(t, filepath.Join(home, "yamn.yml"))
	sysCfg := writeConfigFile(t, systemConfig)

	check := func(name string, f *Flags, want string) {
		t.Helper()
		got, err := f.findConfig()
		if err != nil || got != want {
			t.Errorf("%s: findConfig() = %q, %v; want %q", name, got, err, want)
		}
	}
	check("--config first", &Flags{Config: flagCfg, Dir: dir}, flagCfg)
	t.Setenv("YAMNCFG", envCfg)
	check("then YAMNCFG", &Flags{Dir: dir}, envCfg)
	t.Setenv("YAMNCFG", "")
	check("then --dir", &Flags{Dir: dir}, dirCfg)
	check("then home", &Flags{}, homeCfg)
	os.Remove(homeCfg)
	check("then system", &Flags{}, sysCfg)
	// ./yamn.yml still works when asked for explicitly.
	check("explicit --config ./yamn.yml", &Flags{Config: "yamn.yml"}, "yamn.yml")
}

func TestIgnoredLocalConfig(t *testing.T) {
	cwd, _ := isolateConfigLookup(t)
	if _, ok := IgnoredLocalConfig(""); ok {
		t.Error("No ./yamn.yml: nothing to warn about")
	}
	local := writeConfigFile(t, filepath.Join(cwd, "yamn.yml"))
	if got, ok := IgnoredLocalConfig(""); !ok || filepath.Base(got) != "yamn.yml" {
		t.Errorf("Unused ./yamn.yml should be reported, got %q, %v", got, ok)
	}
	other := writeConfigFile(t, filepath.Join(t.TempDir(), "other.yml"))
	if _, ok := IgnoredLocalConfig(other); !ok {
		t.Error("./yamn.yml unused while another config is in use should be reported")
	}
	if _, ok := IgnoredLocalConfig("yamn.yml"); ok {
		t.Error("./yamn.yml passed with --config is in use: no warning")
	}
	if _, ok := IgnoredLocalConfig(local); ok {
		t.Error("./yamn.yml in use (absolute path): no warning")
	}
}

// TestDefaultCopies checks that --copies is "not given" by default and the
// config's num_copies defaults to 2.
func TestDefaultCopies(t *testing.T) {
	isolateConfigLookup(t)
	c, err := (&Flags{}).ParseConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats.Numcopies != 2 {
		t.Errorf("Default num_copies: got %d, want 2", c.Stats.Numcopies)
	}
}
