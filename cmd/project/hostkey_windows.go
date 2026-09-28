package project

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLockHostKeyFile takes an exclusive lock without blocking, reporting false
// while another process holds it.
func tryLockHostKeyFile(file *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}
