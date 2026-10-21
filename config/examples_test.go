package config

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// decodeStrict decodes a YAML config file into c, failing on unknown keys
// (e.g. typos in the examples).
func decodeStrict(t *testing.T, filename string, c *Config) {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil {
		t.Fatalf("%s: %s", filename, err)
	}
}

// keys returns the dotted paths of all keys in a YAML document.
func keys(t *testing.T, data []byte) (paths []string) {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			if sub, ok := v.(map[string]any); ok {
				walk(prefix+k+".", sub)
				continue
			}
			paths = append(paths, prefix+k)
		}
	}
	walk("", doc)
	slices.Sort(paths)
	return
}

// TestExampleDefaults checks examples/yamn.yml.defaults lists every option
// with its built-in default, so the documentation can't drift.
func TestExampleDefaults(t *testing.T) {
	const file = "../examples/yamn.yml.defaults"
	defaults := (&Flags{}).newConfig()

	// Compare as YAML: an empty list in the file and a nil list in the
	// defaults mean the same.
	fromFile := new(Config)
	decodeStrict(t, file, fromFile)
	got, err := yaml.Marshal(fromFile)
	if err != nil {
		t.Fatal(err)
	}
	want, err := yaml.Marshal(defaults)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s doesn't match the built-in defaults.\nFile:\n%s\nDefaults:\n%s", file, got, want)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := keys(t, want)
	if got := keys(t, data); !slices.Equal(got, wantKeys) {
		t.Errorf("%s doesn't list exactly the config options.\nFile:     %q\nExpected: %q", file, got, wantKeys)
	}
}

// TestExampleConfigsParse checks the minimal examples only use known options.
func TestExampleConfigsParse(t *testing.T) {
	for _, file := range []string{"../examples/yamn.yml.client", "../examples/yamn.yml.remailer"} {
		c := (&Flags{}).newConfig()
		decodeStrict(t, file, c)
	}
}
