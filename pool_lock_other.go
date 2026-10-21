//go:build !unix

package main

// lockPool does nothing where flock isn't available.  Don't run "yamn -S"
// next to a --daemon remailer there.
func lockPool() (unlock func(), err error) {
	return func() {}, nil
}
