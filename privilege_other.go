//go:build !unix && !windows

package main

// privileged has no check on other systems.
func privileged() (string, bool) {
	return "", false
}
