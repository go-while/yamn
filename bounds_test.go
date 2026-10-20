package main

import (
	"fmt"
	"strings"
	"testing"
)

// panicMessage runs f and returns the message of the panic it raised, or ""
// if it didn't panic.
func panicMessage(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	f()
	return
}

func TestSetPlainTextTooLong(t *testing.T) {
	m := newEncMessage()
	m.setChainLength(1)
	if msg := panicMessage(func() { m.setPlainText(make([]byte, bodyBytes)) }); msg != "" {
		t.Fatalf("A body of exactly bodyBytes should be accepted, got panic: %s", msg)
	}
	msg := panicMessage(func() { m.setPlainText(make([]byte, bodyBytes+1)) })
	if !strings.Contains(msg, "exceeds max length") {
		t.Fatalf("A body over bodyBytes should be refused, got: %q", msg)
	}
}

func TestGetPartialIVBounds(t *testing.T) {
	m := newEncMessage()
	m.setChainLength(3) // two intermediate hops: 0 and 1
	if iv := m.getPartialIV(1); len(iv) != 12 {
		t.Fatalf("Last intermediate hop should have a 12 byte partial IV, got %d bytes", len(iv))
	}
	msg := panicMessage(func() { m.getPartialIV(2) })
	if !strings.Contains(msg, "exceeds array length") {
		t.Fatalf("Hop 2 of 2 intermediate hops should be refused, got: %q", msg)
	}
}
