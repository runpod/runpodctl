// Package clierr holds typed errors for failures the cli decides locally, so
// they reach the Execute sink in cmd/root.go carrying a stable machine-readable
// code instead of falling back to the generic "cli_error". The code vocabulary
// itself is documented in internal/api/client.go.
//
// It exists because cmd/root.go's own usage error type is unexported and
// unreachable from the command packages (cmd imports them, not the other way
// round), and a plain fmt.Errorf from a command is indistinguishable from any
// other local failure.
package clierr

import "fmt"

// UsageError marks a caller mistake the cli caught itself — a malformed flag
// value, or a required/conflicting flag combination — as opposed to a runtime or
// api failure. It reports the same "usage_error" code as cobra's own flag and
// argument validation.
//
// Unlike cobra's, it does not trigger a usage dump: these errors name the
// offending flag and often carry a parser message, and a full usage block after
// that is noise on stderr rather than help.
type UsageError struct {
	Err error
}

// Error implements the error interface.
func (e *UsageError) Error() string { return e.Err.Error() }

// Unwrap exposes the wrapped cause.
func (e *UsageError) Unwrap() error { return e.Err }

// ErrorCode returns the stable code for a usage mistake.
func (e *UsageError) ErrorCode() string { return "usage_error" }

// Usagef builds a UsageError with a formatted message.
func Usagef(format string, args ...interface{}) error {
	return &UsageError{Err: fmt.Errorf(format, args...)}
}
