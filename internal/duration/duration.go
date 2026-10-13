// Package duration parses the duration values accepted by cli flags.
//
// It exists so `pod list --since` and `pod/serverless create --wait-timeout`
// share one parser (and one error message) instead of each growing its own.
package duration

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const day = 24 * time.Hour

// maxDays is the largest day count an int64 nanosecond time.Duration holds.
const maxDays = int(math.MaxInt64 / int64(day))

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
		// bound the operand, not the product: time.Duration is int64
		// nanoseconds, so 106752d and up wrap, to a negative value (which a
		// caller guarding only <= 0 would replace with a default, or turn into
		// a --since cutoff in the future) or, from 213504d, past zero to a
		// positive one that is simply wrong (25m for 213504d).
		if n > maxDays {
			return 0, fmt.Errorf("invalid duration %q: out of range (max %dd)", s, maxDays)
		}
		return time.Duration(n) * day, nil
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
