package keymgr

import (
	"bufio"
	"bytes"
	"io"
)

// maxLine is the longest line the keyring and stats readers take.  No
// valid line comes near it.
const maxLine = bufio.MaxScanTokenSize

// lineScanner returns a scanner for the lines of r that skips a line over
// maxLine bytes, as a whole, instead of failing: a bufio.Scanner stops
// with ErrTooLong, so one long entry in a pinger's file would make the
// whole file unusable.  *skipped counts the lines skipped.
func lineScanner(r io.Reader) (scanner *bufio.Scanner, skipped *int) {
	skipped = new(int)
	skipping := false
	split := func(data []byte, atEOF bool) (int, []byte, error) {
		if skipping {
			// Discard up to the end of the long line, so its tail
			// isn't read as a line of its own.
			if i := bytes.IndexByte(data, '\n'); i >= 0 {
				skipping = false
				return i + 1, nil, nil
			}
			return len(data), nil, nil
		}
		advance, token, err := bufio.ScanLines(data, atEOF)
		if advance == 0 && token == nil && err == nil && len(data) >= maxLine {
			// The buffer is full and holds no line end.
			skipping = true
			*skipped++
			return len(data), nil, nil
		}
		return advance, token, err
	}
	scanner = bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), maxLine)
	scanner.Split(split)
	return scanner, skipped
}

// warnSkipped reports the lines lineScanner skipped in the file name.
func warnSkipped(warn warnFunc, name string, skipped *int) {
	if *skipped > 0 {
		warn("%s: %d line(s) over %d bytes skipped", name, *skipped, maxLine)
	}
}
