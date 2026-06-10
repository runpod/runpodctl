package serverless

import (
	"math/rand/v2"
	"testing"
)

// Three implementations of the same 36-char alphanumeric ID generator.
//
// randomString (current, exported via the package) — one rand.IntN call per
// output character. Simple and unbiased.
//
// randomStringBitmask — pulls 64 random bits at a time via rand.Uint64 and
// extracts indices using a rejection-sampling loop (6 bits per attempt; reject
// values >= 36). Amortises source reads across the whole string but does extra
// branching and shift work per index.
//
// randomStringSingleUint64 — pulls one rand.Uint64 and uses repeated %36. Has
// modulo bias (36 doesn't divide 2^64 evenly), but n=7 makes the bias
// negligible for a non-cryptographic uniqueness tag.
//
// All three produce strings from the same alphabet; only the underlying source
// reads and per-character work differ.

const benchLetters = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomStringBitmask(n int) string {
	const (
		alphabetLen = uint64(36)
		bitsPerIdx  = 6
		mask        = uint64(1<<bitsPerIdx - 1) // 0b111111
		// 10 indices per Uint64 (60 bits used; we discard the top 4).
		idxPerWord = 64 / bitsPerIdx
	)
	b := make([]byte, n)
	var (
		cache uint64
		left  int
	)
	for i := 0; i < n; {
		if left == 0 {
			cache = rand.Uint64() //nolint:gosec // non-crypto: template-name uniqueness suffix
			left = idxPerWord
		}
		idx := cache & mask
		cache >>= bitsPerIdx
		left--
		if idx >= alphabetLen {
			continue
		}
		b[i] = benchLetters[idx]
		i++
	}
	return string(b)
}

func randomStringSingleUint64(n int) string {
	const alphabetLen = uint64(36)
	b := make([]byte, n)
	x := rand.Uint64() //nolint:gosec // non-crypto: template-name uniqueness suffix
	for i := range n {
		b[i] = benchLetters[x%alphabetLen]
		x /= alphabetLen
		if x == 0 && i+1 < n {
			// Pull more bits if we've consumed all the entropy from the first
			// Uint64. log_36(2^64) ≈ 12.36, so for n <= 12 we never refill.
			x = rand.Uint64() //nolint:gosec // non-crypto: template-name uniqueness suffix
		}
	}
	return string(b)
}

func BenchmarkRandomString_Current_7(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = randomString(7)
	}
}

func BenchmarkRandomString_Bitmask_7(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = randomStringBitmask(7)
	}
}

func BenchmarkRandomString_SingleUint64_7(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = randomStringSingleUint64(7)
	}
}

// Larger N to amplify per-character cost differences in case the n=7
// measurements are dominated by allocation noise.
func BenchmarkRandomString_Current_64(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = randomString(64)
	}
}

func BenchmarkRandomString_Bitmask_64(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = randomStringBitmask(64)
	}
}

// Correctness check: every produced rune must be in the documented alphabet,
// and the length must match. Doesn't assert uniformity (out of scope), just
// that the alternative implementations don't emit garbage.
func TestRandomStringAlternativesProduceValidChars(t *testing.T) {
	const iterations = 2000
	for _, tc := range []struct {
		name string
		fn   func(int) string
	}{
		{"current", randomString},
		{"bitmask", randomStringBitmask},
		{"single-uint64", randomStringSingleUint64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range iterations {
				s := tc.fn(7)
				if len(s) != 7 {
					t.Fatalf("len = %d, want 7 (%q)", len(s), s)
				}
				for _, r := range s {
					if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
						t.Fatalf("invalid rune %q in %q", r, s)
					}
				}
			}
		})
	}
}
