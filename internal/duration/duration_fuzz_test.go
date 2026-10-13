package duration

import (
	"strconv"
	"strings"
	"testing"
)

// FuzzParse checks that an accepted value is positive and, for the day unit,
// exactly the number of days written: int64 nanoseconds can wrap past zero
// and back to a positive value that is not what the flag said.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"1h", "7d", "30m", "1h30m", "90s", "106751d", "106752d", "213504d",
		"9999999999d", "-1d", "0h", "0d", "abc", "", "10", "1.5d", "+1d",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := Parse(s)
		if err != nil {
			return
		}
		if d <= 0 {
			t.Fatalf("Parse(%q) = %v, want a positive duration", s, d)
		}
		days, ok := strings.CutSuffix(s, "d")
		if !ok {
			return
		}
		n, err := strconv.Atoi(days)
		if err != nil {
			t.Fatalf("Parse(%q) accepted a day count strconv rejects: %v", s, err)
		}
		if d%day != 0 || int64(d/day) != int64(n) {
			t.Fatalf("Parse(%q) = %v, which is not %d days", s, d, n)
		}
	})
}
