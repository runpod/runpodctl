//go:build unix

package project

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockHostKeyFile(file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
