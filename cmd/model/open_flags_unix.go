//go:build unix

// Package model implements the runpodctl model repository commands.
package model

import "syscall"

// openNonblockFlag keeps open(2) from blocking on a FIFO (waiting for a writer)
// that replaced the regular file after the scan. It has no effect on a regular
// file.
const openNonblockFlag = syscall.O_NONBLOCK

// openNoFollowFlag makes open(2) fail instead of following a symlink that
// replaced the final path component. It is used only for opens by path; opens
// through an os.Root are already confined by the root.
const openNoFollowFlag = syscall.O_NOFOLLOW
