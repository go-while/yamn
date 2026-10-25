//go:build unix

package main

import "os"

// privileged reports whether yamn runs as root.
func privileged() (string, bool) {
	return "root", rootIDs(os.Getuid(), os.Geteuid())
}

// rootIDs reports whether the real or the effective user ID is root's.  A
// process whose real ID is root can become root again.
func rootIDs(uid, euid int) bool {
	return uid == 0 || euid == 0
}
