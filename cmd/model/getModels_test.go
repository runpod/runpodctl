package model

import (
	"strconv"
	"testing"
	"time"

	"github.com/runpod/runpodctl/api"
)

func TestModelVersionHash(t *testing.T) {
	tests := []struct {
		name  string
		model *api.Model
		want  string
	}{
		{
			name: "first hash",
			model: &api.Model{Versions: []*api.ModelVersion{
				{Hash: ""},
				{Hash: "  "},
				{Hash: "hash-1"},
				{Hash: "hash-2"},
			}},
			want: "hash-1",
		},
		{
			name: "skips nil version entries",
			model: &api.Model{Versions: []*api.ModelVersion{
				nil,
				{Hash: "hash-after-nil"},
			}},
			want: "hash-after-nil",
		},
		{
			name: "all hashes blank or whitespace",
			model: &api.Model{Versions: []*api.ModelVersion{
				{Hash: ""},
				{Hash: "   "},
				{Hash: "\t\n"},
			}},
			want: "",
		},
		{
			name:  "no versions",
			model: &api.Model{},
			want:  "",
		},
		{
			name:  "nil model",
			model: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modelVersionHash(tt.model); got != tt.want {
				t.Fatalf("modelVersionHash() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValueOrDash(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty -> dash", in: "", want: "-"},
		{name: "whitespace only -> dash", in: "   ", want: "-"},
		{name: "tabs and newlines -> dash", in: "\t\n ", want: "-"},
		{name: "value is preserved", in: "hello", want: "hello"},
		{name: "value with surrounding whitespace is trimmed", in: "  hi  ", want: "hi"},
		{name: "internal whitespace preserved", in: "a b c", want: "a b c"},
		{name: "single char", in: "x", want: "x"},
		{name: "unicode preserved", in: "café", want: "café"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := valueOrDash(tt.in); got != tt.want {
				t.Fatalf("valueOrDash(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatTimestamp(t *testing.T) {
	// Reference epoch values for 2021-01-01T00:00:00Z (chosen for stable RFC3339
	// formatting across precision tiers, no fractional seconds).
	const (
		secs   = int64(1609459200)          // 10 digits
		millis = int64(1609459200000)       // 13 digits
		micros = int64(1609459200000000)    // 16 digits
		nanos  = int64(1609459200000000000) // 19 digits
		want   = "2021-01-01T00:00:00Z"
	)

	tests := []struct {
		name string
		in   string
		want string
	}{
		// positive: each precision tier should round to the same RFC3339 second.
		{name: "seconds (10 digits)", in: "1609459200", want: want},
		{name: "milliseconds (13 digits)", in: "1609459200000", want: want},
		{name: "microseconds (16 digits)", in: "1609459200000000", want: want},
		{name: "nanoseconds (19 digits)", in: "1609459200000000000", want: want},

		// positive: whitespace is trimmed before parsing.
		{name: "trims surrounding whitespace", in: "  1609459200  ", want: want},

		// negative: empty / blank renders as dash.
		{name: "empty -> dash", in: "", want: "-"},
		{name: "whitespace only -> dash", in: "   ", want: "-"},

		// negative: non-numeric is passed through unchanged (ISO strings from
		// the API should not be mangled).
		{name: "ISO 8601 passthrough", in: "2024-06-10T12:00:00Z", want: "2024-06-10T12:00:00Z"},
		{name: "garbage passthrough", in: "not-a-number", want: "not-a-number"},

		// corner: epoch zero across precisions.
		{name: "epoch zero seconds", in: "0", want: "1970-01-01T00:00:00Z"},

		// corner: negative timestamp (pre-epoch).
		{name: "negative seconds (pre-epoch)", in: "-1", want: "1969-12-31T23:59:59Z"},

		// boundary: default-branch path. A length-11 millisecond timestamp
		// (e.g. 99999999999 ms) is < 1e12 so the default branch treats it as
		// seconds — documenting the current (lossy) behavior so a future change
		// of the heuristic is intentional.
		{name: "11-digit value falls through default as seconds", in: "10000000000", want: time.Unix(10000000000, 0).UTC().Format(time.RFC3339)},
		{name: "12-digit value above default threshold treated as ms", in: "1000000000001", want: time.Unix(1000000000, 1*int64(time.Millisecond)).UTC().Format(time.RFC3339)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTimestamp(tt.in); got != tt.want {
				t.Fatalf("formatTimestamp(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	// Sanity-check the precision-tier constants stay aligned: every tier must
	// resolve to the same RFC3339 instant when fed through formatTimestamp.
	for _, v := range []int64{secs, millis, micros, nanos} {
		got := formatTimestamp(strconv.FormatInt(v, 10))
		if got != want {
			t.Fatalf("precision tier %d produced %q, want %q", v, got, want)
		}
	}
}
