package main

import (
	"strings"
	"testing"
)

// TestPrivilegeVerdict checks that a privileged run is refused without
// --i-am-root and warned about with it, and an unprivileged one is neither.
func TestPrivilegeVerdict(t *testing.T) {
	for _, c := range []struct {
		privileged, allow, refuse, warn bool
	}{
		{false, false, false, false},
		{false, true, false, false},
		{true, false, true, false},
		{true, true, false, true},
	} {
		warning, refuse := privilegeVerdict("root", c.privileged, c.allow)
		if refuse != c.refuse || (warning != "") != c.warn {
			t.Errorf("privileged %v, --i-am-root %v: refuse %v, warning %q", c.privileged, c.allow, refuse, warning)
		}
		if c.warn && !strings.Contains(warning, "root") {
			t.Errorf("warning %q doesn't say who", warning)
		}
	}
}
