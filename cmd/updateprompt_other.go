//go:build !unix

package cmd

import (
	"os"
	"time"
)

// waitReadable has no portable way to bound a console read here, so the prompt
// waits for an answer as it would without a timeout.
func waitReadable(*os.File, time.Duration) bool { return true }
