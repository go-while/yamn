package main

import (
	"fmt"
	"os"
)

// checkPrivileged ends yamn when it runs as root, or on Windows as an
// administrator, unless allow (--i-am-root) is set; then it returns a
// warning to log.  yamn never needs those rights: a remailer reads mail sent
// by anyone and only writes its own files, so a bug in it should reach no
// more than those files.
func checkPrivileged(allow bool) (warning string) {
	who, ok := privileged()
	warning, refuse := privilegeVerdict(who, ok, allow)
	if refuse {
		fmt.Fprintf(os.Stderr, "yamn won't run as %s.  Run it as an unprivileged user, "+
			"or give --i-am-root; see RUNNING YAMN in yamn(1).\n", who)
		os.Exit(1)
	}
	return warning
}

// privilegeVerdict decides for checkPrivileged: refuse a privileged run
// without allow, warn about one with it.
func privilegeVerdict(who string, privileged, allow bool) (warning string, refuse bool) {
	switch {
	case !privileged:
		return "", false
	case !allow:
		return "", true
	}
	return fmt.Sprintf("Running as %s (--i-am-root): a bug in yamn could reach the whole system", who), false
}
