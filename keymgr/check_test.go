package keymgr

import (
	"strings"
	"testing"
)

const htmlErrorPage = "<!DOCTYPE html>\n<html><body><h1>Service Unavailable</h1></body></html>\n"

func TestCheckPubring(t *testing.T) {
	pubring := fakePubring()
	if err := CheckPubring([]byte(pubring)); err != nil {
		t.Errorf("Valid pubring rejected: %s", err)
	}
	// Cut the file off in the middle of the first key block.
	truncated := pubring[:strings.Index(pubring, "-----End Mix Key-----")]
	for name, b := range map[string]string{
		"empty":     "",
		"HTML page": htmlErrorPage,
		"truncated": truncated,
		"no header": "-----Begin Mix Key-----\nabc\ndef\n-----End Mix Key-----\n",
	} {
		if err := CheckPubring([]byte(b)); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
}

func TestCheckStats(t *testing.T) {
	if err := CheckStats([]byte(fakeMlist2())); err != nil {
		t.Errorf("Valid stats rejected: %s", err)
	}
	for name, b := range map[string]string{
		"empty":         "",
		"HTML page":     htmlErrorPage,
		"bad timestamp": "Generated: yesterday\n" + strings.Repeat("-", 64) + "\n",
		"no table":      "Generated: Mon 02 Jan 2006 15:04:05 GMT\n",
	} {
		if err := CheckStats([]byte(b)); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
}
