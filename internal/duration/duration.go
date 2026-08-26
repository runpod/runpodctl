// Package duration parses the duration values accepted by cli flags.
//
// It exists so `pod list --since` and `pod/serverless create --wait-timeout`
// share one parser (and one error message) instead of each growing its own.
package duration

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Parse parses a duration string like "30m", "1h", "1h30m" or "7d". It handles
// "d" (days) itself because time.ParseDuration has no day unit, and rejects
// zero or negative values: every cli flag using this means "a span of time from
// now", which a non-positive value cannot express.
func Parse(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		if n <= 0 {
			return 0, fmt.Errorf("invalid duration %q: must be positive", s)
		}
		// check the product, not just the operand: time.Duration is int64
		// nanoseconds, so 106752d and up wrap to a negative value. A caller that
		// only guards its own input against <= 0 would then silently substitute a
		// default (waitfor.Until) or put a --since cutoff in the future.
		d := time.Duration(n) * 24 * time.Hour
		if d <= 0 {
			return 0, fmt.Errorf("invalid duration %q: out of range (max 106751d)", s)
		}
		return d, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: supported formats are e.g. 30m, 2h, 7d", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid duration %q: must be positive", s)
	}
	return d, nil
}

// ParseDeadline resolves a flag value that names a point in time into an
// absolute UTC instant. It accepts both forms users reach for: a relative
// duration ("2h", "7d") and an RFC 3339 timestamp ("2026-04-15T00:00:00Z").
//
// Past instants are rejected. The pod scheduler accepts them and then never
// fires, which reads as a timer that was silently ignored -- the exact failure
// this validation exists to prevent.
func ParseDeadline(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty value: use a duration like 2h or an rfc3339 timestamp like 2026-04-15T00:00:00Z")
	}

	if d, err := Parse(s); err == nil {
		return now.UTC().Add(d), nil
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time %q: use a duration like 2h, 7d or an rfc3339 timestamp like 2026-04-15T00:00:00Z", s)
	}
	if !t.After(now) {
		return time.Time{}, fmt.Errorf("invalid time %q: must be in the future (now is %s)", s, now.UTC().Format(time.RFC3339))
	}
	return t.UTC(), nil
}
