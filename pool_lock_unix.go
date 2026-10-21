//go:build unix

package main

import (
	"errors"
	"os"
	"path"
	"syscall"
)

// lockPool takes an exclusive lock on <pooldir>/.lock, so only one process
// sends mail from the pool at a time: "yamn -S" next to a --daemon remailer
// could otherwise both send a file before either deletes it.  The lock is
// released by calling unlock, or when the process ends.  errPoolLocked means
// another process holds it.
func lockPool() (unlock func(), err error) {
	f, err := os.OpenFile(path.Join(cfg.Files.Pooldir, ".lock"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			err = errPoolLocked
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}
