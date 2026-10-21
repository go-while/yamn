package linebreaker

import (
	"bytes"
	"testing"
)

// TestLineLengthBelowOne checks that a line length below 1 is an error
// instead of endless recursion.
func TestLineLengthBelowOne(t *testing.T) {
	for _, length := range []int{0, -1} {
		var out bytes.Buffer
		lb := NewLineBreaker(&out, length)
		if _, err := lb.Write([]byte("abc")); err == nil {
			t.Errorf("Line length %d: Write should fail", length)
		}
	}
}
