package main

import (
	"fmt"
	"os"
)

// refusePrivileged ends yamn when it runs as root, or on Windows as an
// administrator.  It never needs those rights: a remailer reads mail sent by
// anyone and only writes its own files, so a bug in it should reach no more
// than those files.
func refusePrivileged() {
	if who, ok := privileged(); ok {
		fmt.Fprintf(os.Stderr, "yamn won't run as %s.  Run it as an unprivileged user; "+
			"see RUNNING YAMN in yamn(1).\n", who)
		os.Exit(1)
	}
}
