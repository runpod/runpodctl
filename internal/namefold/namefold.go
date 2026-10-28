// Package namefold holds the rule for when two file names may be one file on
// disk. Both receive (cmd/transfer) and network volume downloads
// (internal/nvtransfer) refuse writes that would collide under it, so it lives
// in one place.
package namefold

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Key is the index key under which two spellings that a filesystem may treat
// as one name collide. NFC comes first because folding does not normalize, and
// APFS compares names normalization-insensitively, so "é" and "é" are one
// file there. Then full unicode case folding; strings.ToLower is not enough: it
// leaves ſ (U+017F) and ς (U+03C2) as they are, while a case-insensitive
// filesystem reads them as s and σ.
//
// The key over-approximates on purpose: full folding maps ß to ss, and a
// case-insensitive filesystem that keeps normalization forms apart (NTFS) is
// still treated as normalizing. Both only ever refuse.
func Key(name string) string {
	// a Caser is stateful and must not be shared between goroutines
	return cases.Fold().String(norm.NFC.String(name))
}
