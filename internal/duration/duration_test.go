package duration

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		// valid inputs
		{input: "1h", want: time.Hour},
		{input: "7d", want: 7 * 24 * time.Hour},
		{input: "30m", want: 30 * time.Minute},
		{input: "1h30m", want: time.Hour + 30*time.Minute},
		{input: "2h", want: 2 * time.Hour},
		{input: "1d", want: 24 * time.Hour},
		{input: "90s", want: 90 * time.Second},
		{input: "5s", want: 5 * time.Second},
		// the largest day count that still fits in an int64 nanosecond duration.
		{input: "106751d", want: 106751 * 24 * time.Hour},

		// invalid inputs
		// int64 nanoseconds overflow at 106752 days: the product wraps negative, and
		// a caller that only guards its own input against <= 0 would silently
		// substitute a default instead of rejecting the flag.
		{input: "106752d", wantErr: true},
		{input: "200000d", wantErr: true},
		{input: "9999999999d", wantErr: true},
		{input: "-1d", wantErr: true},
		{input: "0h", wantErr: true},
		{input: "0d", wantErr: true},
		{input: "abc", wantErr: true},
		{input: "", wantErr: true},
		{input: "-2h", wantErr: true},
		{input: "10", wantErr: true},
		{input: "1.5d", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("Parse(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDeadline(t *testing.T) {
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "30m", want: "2026-04-15T12:30:00Z"},
		{in: "7d", want: "2026-04-22T12:00:00Z"},
		{in: " 2h ", want: "2026-04-15T14:00:00Z"},
		{in: "2026-04-15T16:30:00+02:00", want: "2026-04-15T14:30:00Z"},
		{in: "", wantErr: true},
		{in: "tomorrow", wantErr: true},
		{in: "-2h", wantErr: true},
		{in: "2020-01-01T00:00:00Z", wantErr: true},
		{in: "2026-04-15T12:00:00Z", wantErr: true}, // now is not in the future
	}

	for _, tc := range cases {
		got, err := ParseDeadline(tc.in, now)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseDeadline(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDeadline(%q) errored: %v", tc.in, err)
			continue
		}
		if got.Format(time.RFC3339) != tc.want {
			t.Errorf("ParseDeadline(%q) = %s, want %s", tc.in, got.Format(time.RFC3339), tc.want)
		}
	}
}
