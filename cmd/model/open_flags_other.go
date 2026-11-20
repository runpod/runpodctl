//go:build !unix

// Package model implements the runpodctl model repository commands.
package model

// Without O_NONBLOCK or O_NOFOLLOW (Windows has no FIFOs a path can be swapped
// for), the open relies on the lstat before it and the fstat after it.
const (
	openNonblockFlag = 0
	openNoFollowFlag = 0
)
