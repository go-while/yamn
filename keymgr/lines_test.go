package keymgr

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLineScanner checks that lines up to maxLine bytes are read, longer
// ones skipped whole, with or without CR and at the end of the input.
func TestLineScanner(t *testing.T) {
	fits := strings.Repeat("a", maxLine-1)
	long := strings.Repeat("b", maxLine)
	huge := strings.Repeat("c", 5*maxLine+17)
	for _, c := range []struct {
		name, in string
		want     []string
		skipped  int
	}{
		{"short", "x\ny\n", []string{"x", "y"}, 0},
		{"no final newline", "x\ny", []string{"x", "y"}, 0},
		{"CRLF", "x\r\ny\r\n", []string{"x", "y"}, 0},
		{"longest line read", fits + "\nz\n", []string{fits, "z"}, 0},
		{"too long", "x\n" + long + "\nz\n", []string{"x", "z"}, 1},
		{"tail not read", "x\n" + huge + "\nz\n", []string{"x", "z"}, 1},
		{"long last line", "x\n" + huge, []string{"x"}, 1},
		{"two long lines", long + "\n" + huge + "\nz", []string{"z"}, 2},
	} {
		s, skipped := lineScanner(strings.NewReader(c.in))
		var got []string
		for s.Scan() {
			got = append(got, s.Text())
		}
		if err := s.Err(); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) || *skipped != c.skipped {
			t.Errorf("%s: got %d lines %.40q, skipped %d; want %d lines, skipped %d",
				c.name, len(got), got, *skipped, len(c.want), c.skipped)
		}
	}
}

// TestPubringLongLine checks that a line over maxLine bytes costs only
// its own entry: a key with a huge caps field is skipped and the other
// keys, the download check and Headers still work.  A long line ending
// in a valid key header isn't read as that header.
func TestPubringLongLine(t *testing.T) {
	bloated := alpha
	bloated.caps = strings.Repeat("C", 70000)
	hidden := testPubring(gamma)
	ring := bytes.Join([][]byte{
		testPubring(bloated),
		testPubring(beta),
		append([]byte(strings.Repeat("x", 70000)), hidden...), // gamma's header is the tail of a long line
	}, nil)

	var warnings []string
	rems, err := readPubring(bytes.NewReader(ring), mergeNow, false, func(f string, a ...any) {
		warnings = append(warnings, fmt.Sprintf(f, a...))
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rems) != 1 || rems[0].name != "beta" {
		var names []string
		for _, r := range rems {
			names = append(names, r.name)
		}
		t.Errorf("keys %v, want only beta", names)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "2 line(s) over") {
		t.Errorf("warnings %q, want one about 2 long lines", warnings)
	}
	if err := checkPubring(ring, mergeNow, false); err != nil {
		t.Errorf("checkPubring: %v", err)
	}

	file := filepath.Join(t.TempDir(), "pubring.mix")
	if err := os.WriteFile(file, ring, 0644); err != nil {
		t.Fatal(err)
	}
	headers, err := Headers(file)
	if err != nil || len(headers) != 1 || !strings.HasPrefix(headers[0], "beta ") {
		t.Errorf("Headers: %d headers, err %v; want beta's", len(headers), err)
	}
}

// TestStatsLongLine checks that a stats row over maxLine bytes is skipped
// and the other rows still read.
func TestStatsLongLine(t *testing.T) {
	stats := string(testStats(time.Hour, "alpha 10 100.0", "beta 70 99.0"))
	stats = strings.Replace(stats, "%  D\n", "%  D"+strings.Repeat("D", 70000)+"\n", 1)
	var warnings []string
	_, rows, err := readStats(strings.NewReader(stats), "mlist2.txt", func(f string, a ...any) {
		warnings = append(warnings, fmt.Sprintf(f, a...))
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].name != "beta" {
		t.Errorf("rows %+v, want only beta", rows)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "mlist2.txt: 1 line(s) over") {
		t.Errorf("warnings %q", warnings)
	}
	if err := CheckStats([]byte(stats)); err != nil {
		t.Errorf("CheckStats: %v", err)
	}
}
