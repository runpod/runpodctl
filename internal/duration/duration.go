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
		return time.Duration(n) * 24 * time.Hour, nil
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
