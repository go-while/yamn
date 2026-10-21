package config

import (
	"flag"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestManPageSynopsis checks the options in the SYNOPSIS of doc/yamn.1:
// each must exist, and take an argument exactly if it isn't a switch.
func TestManPageSynopsis(t *testing.T) {
	data, err := os.ReadFile("../doc/yamn.1")
	if err != nil {
		t.Fatal(err)
	}
	_, synopsis, ok := strings.Cut(string(data), ".SH SYNOPSIS\n")
	if !ok {
		t.Fatal("No SYNOPSIS in doc/yamn.1")
	}
	synopsis, _, _ = strings.Cut(synopsis, "\n.SH ")

	oldCommandLine, oldArgs := flag.CommandLine, os.Args
	defer func() { flag.CommandLine, os.Args = oldCommandLine, oldArgs }()
	flag.CommandLine = flag.NewFlagSet("yamn", flag.ContinueOnError)
	os.Args = []string{"yamn"}
	ParseFlags()

	// An option, and the argument that follows it, if any.
	option := regexp.MustCompile(`-{1,2}([A-Za-z][\w-]*)( [^\s\[\]-][^\[\]]*)?`)
	found := 0
	for _, m := range option.FindAllStringSubmatch(synopsis, -1) {
		found++
		f := flag.CommandLine.Lookup(m[1])
		if f == nil {
			t.Errorf("SYNOPSIS: %q is not an option", m[0])
			continue
		}
		b, isSwitch := f.Value.(interface{ IsBoolFlag() bool })
		isSwitch = isSwitch && b.IsBoolFlag()
		if hasArg := m[2] != ""; hasArg == isSwitch {
			t.Errorf("SYNOPSIS: %q: -%s (%s) %s an argument", m[0], m[1], f.Usage,
				map[bool]string{true: "takes no", false: "needs"}[isSwitch])
		}
	}
	if found == 0 {
		t.Error("No options found in the SYNOPSIS")
	}
}
