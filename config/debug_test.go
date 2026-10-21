package config

import (
	"bytes"
	"testing"
)

// TestDebugHidesPassword checks that --debug doesn't print the SMTP
// password, only that one is set.
func TestDebugHidesPassword(t *testing.T) {
	c := (&Flags{}).newConfig()
	c.Mail.Password = "s3cret-pw"
	out, err := c.Debug()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("s3cret-pw")) {
		t.Errorf("--debug prints the SMTP password:\n%s", out)
	}
	if !bytes.Contains(out, []byte("password: (hidden)")) {
		t.Errorf("--debug should show that a password is set:\n%s", out)
	}
	if c.Mail.Password != "s3cret-pw" {
		t.Errorf("Debug changed the config's password to %q", c.Mail.Password)
	}

	c.Mail.Password = ""
	out, err = c.Debug()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(`password: ""`)) {
		t.Errorf("--debug should show an empty password as empty:\n%s", out)
	}
}
