package transfer

import (
	"encoding/binary"
	"math"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/schollz/croc/v9/src/models"
	"golang.org/x/text/unicode/norm"
)

// FuzzFoldKey checks the aliases foldKey exists to collide: the NFC and NFD
// spellings of a name, and its ASCII case variants, which every
// case-insensitive filesystem agrees on.
func FuzzFoldKey(f *testing.F) {
	for _, s := range []string{"a", "Readme.md", "d/ſ", "d/s", "é", "é", "ß", "ı", "K", "K", "Å/x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			return
		}
		key := foldKey(s)
		swapped, _ := swapASCIICase(s)
		for _, alias := range []string{norm.NFC.String(s), norm.NFD.String(s), swapped} {
			if got := foldKey(alias); got != key {
				t.Fatalf("foldKey(%q) = %q, but its alias %q folds to %q", s, key, alias, got)
			}
		}
	})
}

// FuzzValidateManifest declares two files, either of which may be a symlink,
// into an empty destination. Whatever validateManifest accepts is then created
// through the root exactly as declared, and nothing may resolve outside it:
// os.Root creates an escaping link without complaint, so this is the property
// the validator alone provides.
func FuzzValidateManifest(f *testing.F) {
	for _, seed := range [][6]string{
		{"", "a", "", "sub", "b", ""},
		{"", "esc", "../outside", "", "b", ""},
		{"d", "link", "../x", "", "x", ""},
		{"d", "ſ", "..", "d/s", "pwn", "../.."},
		{"", "sub", "dir", "sub", "esc", "../.."},
		{"", "pwn", "sub/esc", "", "sub", "."},
		{"./a/../b", "c", "", "/abs", "d", ""},
		{"", "abs", "/etc", "", "x", "a\\..\\.."},
	} {
		f.Add(seed[0], seed[1], seed[2], seed[3], seed[4], seed[5])
	}
	f.Fuzz(func(t *testing.T, folderA, nameA, linkA, folderB, nameB, linkB string) {
		dest := t.TempDir()
		root, err := os.OpenRoot(dest)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		info := SenderInfo{FilesToTransfer: []FileInfo{
			{FolderRemote: folderA, Name: nameA, Symlink: linkA},
			{FolderRemote: folderB, Name: nameB, Symlink: linkB},
		}}
		paths, _, err := validateManifest(root, info)
		if err != nil {
			return
		}
		for i, rel := range paths {
			if !filepath.IsLocal(filepath.FromSlash(rel)) {
				t.Fatalf("file %d accepted at %q", i, rel)
			}
			if parent := path.Dir(rel); parent != "." {
				_ = root.MkdirAll(parent, 0o700)
			}
			if target := info.FilesToTransfer[i].Symlink; target != "" {
				_ = root.Symlink(target, rel)
			} else if file, err := root.Create(rel); err == nil {
				file.Close()
			}
		}
		for i, rel := range paths {
			if _, err := root.Stat(rel); escapesRoot(err) {
				t.Fatalf("file %d at %q resolves outside the destination (manifest %+v)", i, rel, info.FilesToTransfer)
			}
		}
	})
}

// FuzzValidateChunkRanges decodes the peer's resume request from raw bytes and
// checks that anything accepted is well-formed and names no more chunks than
// the file has, summed without overflow.
func FuzzValidateChunkRanges(f *testing.F) {
	const chunk = models.TCP_BUFFER_SIZE / 2
	for _, ranges := range [][]int64{
		{},
		{chunk, 0, 1},
		{chunk, 0, 3},
		{chunk, 0, 1, chunk * 2, 1},
		{chunk, 0},
		{0, 0, 1},
		{chunk, 0, 1 << 40},
		{chunk, 0, 1, chunk, math.MaxInt64},
	} {
		f.Add(encodeInt64s(ranges), int64(3*chunk))
	}
	f.Fuzz(func(t *testing.T, data []byte, size int64) {
		ranges := decodeInt64s(data)
		if validateChunkRanges(ranges, size) != nil || len(ranges) == 0 {
			return
		}
		if len(ranges)%2 != 1 || ranges[0] <= 0 {
			t.Fatalf("accepted malformed ranges %v", ranges)
		}
		total := new(big.Int)
		for i := 1; i < len(ranges); i += 2 {
			if ranges[i] < 0 || ranges[i+1] < 0 {
				t.Fatalf("accepted negative values in %v", ranges)
			}
			total.Add(total, big.NewInt(ranges[i+1]))
		}
		limit := min(fileChunks(size), maxResumeChunks)
		if total.Cmp(big.NewInt(limit)) > 0 {
			t.Fatalf("accepted %v chunks of a file with %d (size %d): %v", total, limit, size, ranges)
		}
	})
}

func encodeInt64s(values []int64) []byte {
	out := make([]byte, 0, 8*len(values))
	for _, v := range values {
		out = binary.LittleEndian.AppendUint64(out, uint64(v))
	}
	return out
}

func decodeInt64s(data []byte) []int64 {
	values := make([]int64, 0, len(data)/8)
	for len(data) >= 8 {
		values = append(values, int64(binary.LittleEndian.Uint64(data)))
		data = data[8:]
	}
	return values
}
