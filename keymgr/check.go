package keymgr

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"time"
)

// CheckPubring checks that b looks like a pubring.mix file: at least one key
// block with a header line and Begin/End cutmarks.  It's used to reject a
// download (an HTML error page, an empty or truncated file) before it
// replaces a working keyring.
func CheckPubring(b []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(b))
	gotHeader, inKey := false, false
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case len(strings.Split(line, " ")) == 7:
			gotHeader = true
		case line == "-----Begin Mix Key-----" && gotHeader:
			inKey = true
		case line == "-----End Mix Key-----" && inKey:
			return nil
		}
	}
	return errors.New("no complete key block found")
}

// CheckStats checks that b looks like an mlist2.txt stats file: a valid
// Generated timestamp followed by the dashed line before the stats table.
func CheckStats(b []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(b))
	gotGenerated := false
	for scanner.Scan() {
		line := scanner.Text()
		if stamp, ok := strings.CutPrefix(line, "Generated: "); ok {
			if _, err := time.Parse(generatedFormat, stamp); err != nil {
				return errors.New("invalid Generated timestamp")
			}
			gotGenerated = true
		} else if gotGenerated && strings.HasPrefix(line, "----------") {
			return nil
		}
	}
	return errors.New("no Generated timestamp and stats table found")
}
