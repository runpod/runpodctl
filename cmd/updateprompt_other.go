//go:build !unix

package cmd

import (
	"os"
	"time"
)

// promptCanTimeOut is false here: there is no portable way to bound a console
// read, so interactive sessions get the one-line notice instead of the prompt.
var promptCanTimeOut = false

// waitReadable cannot bound the wait. it is unused while promptCanTimeOut is
// false.
func waitReadable(*os.File, time.Duration) bool { return true }

// isForeground has no process-group check here.
func isForeground(int) bool { return true }

// dirWritable assumes the directory is writable; a failed update reports the
// error instead.
func dirWritable(string) bool { return true }
