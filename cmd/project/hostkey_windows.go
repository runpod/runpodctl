package project

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// droppedConnectionErrors are the dial failures a pod's dying container
// produces: refusing the connection, or resetting one it had accepted. the
// net package surfaces winsock's codes, not the posix-named syscall ones.
var droppedConnectionErrors = []error{windows.WSAECONNREFUSED, windows.WSAECONNRESET}

// tryLockHostKeyFile takes an exclusive lock without blocking, reporting false
// while another process holds it.
func tryLockHostKeyFile(file *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}
