//go:build unix

package project

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockHostKeyFile takes an exclusive lock without blocking, reporting false
// while another process holds it.
func tryLockHostKeyFile(file *os.File) (bool, error) {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, unix.EWOULDBLOCK):
			return false, nil
		case !errors.Is(err, unix.EINTR):
			return false, err
		}
	}
}
