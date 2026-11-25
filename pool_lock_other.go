//go:build !unix

package main

// lockPool does nothing where flock isn't available.  Don't run "yamn -S"
// next to a remailer there (--daemon, or a single "yamn -M" run, which
// sends a pool batch too).
func lockPool() (unlock func(), err error) {
	return func() {}, nil
}
