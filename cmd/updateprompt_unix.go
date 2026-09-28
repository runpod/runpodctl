//go:build unix

package cmd

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// waitReadable reports whether f has input within d. it polls instead of
// setting a read deadline: go opens a terminal stdin in blocking mode, where
// SetReadDeadline returns os.ErrNoDeadline, and switching the fd to
// non-blocking would leak into the parent shell if the process were killed
// mid-prompt. an unexpected poll error falls back to a plain blocking read.
func waitReadable(f *os.File, d time.Duration) bool {
	for {
		if d <= 0 {
			return false
		}
		fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}}
		start := time.Now()
		n, err := unix.Poll(fds, int(d.Milliseconds()))
		if errors.Is(err, unix.EINTR) {
			d -= time.Since(start)
			continue
		}
		if err != nil {
			return true
		}
		return n > 0
	}
}
