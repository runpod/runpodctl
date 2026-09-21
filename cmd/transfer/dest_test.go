package transfer

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schollz/croc/v9/src/utils"
)

// destPair returns a destination to receive into and a sibling directory that
// must stay untouched, so every escape assertion is "the outside file does not
// exist" rather than "the transfer failed" -- which would pass against the
// unfixed code too.
func destPair(t *testing.T) (dest, outside string) {
	t.Helper()
	base := t.TempDir()
	dest = filepath.Join(base, "dest")
	outside = filepath.Join(base, "outside")
	for _, dir := range []string{dest, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}
	return dest, outside
}

func openTestDest(t *testing.T, dir string) *confinedDest {
	t.Helper()
	d, err := openDest(dir)
	if err != nil {
		t.Fatalf("openDest(%s): %v", dir, err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpenForWriteRefusesEscapingSymlink(t *testing.T) {
	dest, outside := destPair(t)
	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(dest, "link")); err != nil {
		t.Fatalf("seeding symlink: %v", err)
	}
	d := openTestDest(t, dest)

	f, _, err := d.openForWrite("link")
	if err == nil {
		f.Close()
		t.Fatal("openForWrite followed a symlink out of the destination")
	}
	if _, err := os.Lstat(filepath.Join(outside, "target")); err == nil {
		t.Error("a file was created outside the destination")
	}
}

func TestMkdirAllRefusesEscapingSymlink(t *testing.T) {
	dest, outside := destPair(t)
	if err := os.Symlink(outside, filepath.Join(dest, "sub")); err != nil {
		t.Fatalf("seeding symlink: %v", err)
	}
	d := openTestDest(t, dest)

	if err := d.mkdirAll("sub/deep"); err == nil {
		t.Fatal("mkdirAll descended a symlink out of the destination")
	}
	if _, err := os.Lstat(filepath.Join(outside, "deep")); err == nil {
		t.Error("a directory was created outside the destination")
	}
}

func TestOpenForWriteRefusesTraversal(t *testing.T) {
	dest, outside := destPair(t)
	d := openTestDest(t, dest)

	for _, rel := range []string{"../outside/pwn", "/etc/pwn"} {
		if f, _, err := d.openForWrite(rel); err == nil {
			f.Close()
			t.Errorf("openForWrite(%q) succeeded", rel)
		}
	}
	if _, err := os.Lstat(filepath.Join(outside, "pwn")); err == nil {
		t.Error("a file was created outside the destination")
	}
}

// TestSymlinkCanEscapeTheRoot documents the gap that makes prevalidation
// load-bearing: os.Root refuses to *follow* a link out of the tree, but it
// creates one without complaint, and the link outlives the transfer. If this
// ever starts failing, os.Root grew a guard and validateManifest's target check
// became belt-and-braces rather than the only thing standing there.
func TestSymlinkCanEscapeTheRoot(t *testing.T) {
	dest, outside := destPair(t)
	d := openTestDest(t, dest)

	if err := d.symlink("../outside/secret", "esc"); err != nil {
		t.Skipf("os.Root now refuses to create an escaping symlink (%v); validateManifest still refuses it earlier", err)
	}
	target, err := os.Readlink(filepath.Join(dest, "esc"))
	if err != nil {
		t.Fatalf("reading back the link: %v", err)
	}
	if target != "../outside/secret" {
		t.Errorf("link target = %q, want ../outside/secret", target)
	}
	// writing *through* it is still refused, which is why nothing escapes
	// during the transfer itself.
	if f, _, err := d.openForWrite("esc"); err == nil {
		f.Close()
		t.Error("wrote through an escaping symlink")
	}
	if _, err := os.Lstat(filepath.Join(outside, "secret")); err == nil {
		t.Error("a file was created outside the destination")
	}
}

// buildZip writes an archive into the destination so extractZip can read it
// back the way a received TempFile arrives.
func buildZip(t *testing.T, dest, name string, entries map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for entryName, content := range entries {
		f, err := w.Create(entryName)
		if err != nil {
			t.Fatalf("adding %s: %v", entryName, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("writing %s: %v", entryName, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dest, name), buf.Bytes(), 0o600); err != nil {
		t.Fatalf("writing archive: %v", err)
	}
}

func TestExtractZipRefusesSlipAndWritesNothing(t *testing.T) {
	dest, outside := destPair(t)
	// the legitimate-looking entry is listed too: extraction validates the whole
	// archive first, so neither entry may land.
	buildZip(t, dest, "payload.zip", map[string]string{
		"../outside/escape.txt": "pwned",
		"innocent.txt":          "fine",
	})
	d := openTestDest(t, dest)

	if err := d.extractZip("payload.zip"); err == nil {
		t.Fatal("extractZip accepted an archive with a traversing entry")
	}
	if _, err := os.Lstat(filepath.Join(outside, "escape.txt")); err == nil {
		t.Error("archive wrote outside the destination")
	}
	if _, err := os.Lstat(filepath.Join(dest, "innocent.txt")); err == nil {
		t.Error("archive was partly extracted; validation must precede the first write")
	}
}

func TestExtractZipWritesEntries(t *testing.T) {
	dest, _ := destPair(t)
	buildZip(t, dest, "payload.zip", map[string]string{
		"pkg/a.txt":     "alpha",
		"pkg/sub/b.txt": "beta",
	})
	d := openTestDest(t, dest)

	if err := d.extractZip("payload.zip"); err != nil {
		t.Fatalf("extractZip: %v", err)
	}
	for rel, want := range map[string]string{
		"pkg/a.txt":     "alpha",
		"pkg/sub/b.txt": "beta",
	} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("reading %s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
}

// TestExtractZipOverwritesWithoutPrompting matters because upstream's extractor
// reads stdin mid-extraction, which would hang a non-interactive receive.
func TestExtractZipOverwritesWithoutPrompting(t *testing.T) {
	dest, _ := destPair(t)
	if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}
	buildZip(t, dest, "payload.zip", map[string]string{"a.txt": "new"})
	d := openTestDest(t, dest)

	if err := d.extractZip("payload.zip"); err != nil {
		t.Fatalf("extractZip: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil {
		t.Fatalf("reading a.txt: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("a.txt = %q, want the archive's content", got)
	}
}

// TestHashFileMatchesUpstream is the compatibility check that keeps resume and
// skip working: the sender hashes with utils.HashFile, so a receiver computing
// different bytes for identical content would re-download every time.
func TestHashFileMatchesUpstream(t *testing.T) {
	dest, _ := destPair(t)

	payload := make([]byte, 300*1024) // larger than one chunk, and not all zero
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("generating payload: %v", err)
	}
	for name, content := range map[string][]byte{
		"empty.bin": {},
		"small.bin": []byte("alpha beta gamma"),
		"large.bin": payload,
	} {
		if err := os.WriteFile(filepath.Join(dest, name), content, 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	if err := os.Symlink("small.bin", filepath.Join(dest, "link")); err != nil {
		t.Fatalf("seeding symlink: %v", err)
	}

	d := openTestDest(t, dest)
	for _, algorithm := range []string{"xxhash", "md5"} {
		for _, name := range []string{"empty.bin", "small.bin", "large.bin", "link"} {
			want, err := utils.HashFile(filepath.Join(dest, name), algorithm)
			if err != nil {
				t.Fatalf("utils.HashFile(%s, %s): %v", name, algorithm, err)
			}
			got, err := d.hashFile(name, algorithm)
			if err != nil {
				t.Fatalf("hashFile(%s, %s): %v", name, algorithm, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s/%s: hash = %x, want %x", name, algorithm, got, want)
			}
		}
	}

	// the empty algorithm is croc's default of xxhash.
	want, err := utils.HashFile(filepath.Join(dest, "small.bin"), "xxhash")
	if err != nil {
		t.Fatalf("utils.HashFile: %v", err)
	}
	got, err := d.hashFile("small.bin", "")
	if err != nil {
		t.Fatalf("hashFile with the default algorithm: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("default algorithm hash = %x, want xxhash %x", got, want)
	}

	// imohash has no streaming or ReaderAt api, so it is reported rather than
	// computed by reading a whole received file into memory. the caller treats
	// that like any hash failure and re-transfers.
	if _, err := d.hashFile("small.bin", "imohash"); err == nil {
		t.Error("imohash reported a hash; it cannot be computed without buffering the file")
	}
}

func TestMissingChunksMatchesUpstream(t *testing.T) {
	dest, _ := destPair(t)
	const chunkSize = 1024

	// a sparse file: written regions interleaved with all-zero holes, which is
	// what a partially received file looks like.
	content := make([]byte, chunkSize*8)
	for _, region := range [][2]int{{0, chunkSize}, {chunkSize * 3, chunkSize * 5}} {
		for i := region[0]; i < region[1]; i++ {
			content[i] = 0xAB
		}
	}
	name := "partial.bin"
	full := filepath.Join(dest, name)
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}

	d := openTestDest(t, dest)
	size := int64(len(content))
	want := utils.MissingChunks(full, size, chunkSize)
	got := d.missingChunks(name, size, chunkSize)
	if len(got) != len(want) {
		t.Fatalf("missingChunks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("missingChunks = %v, want %v", got, want)
		}
	}

	// a size mismatch yields nothing from both, which is how a resume decides
	// to start over.
	if chunks := d.missingChunks(name, size+1, chunkSize); len(chunks) != 0 {
		t.Errorf("missingChunks with a wrong size = %v, want empty", chunks)
	}
}

func TestCreateTempStaysInTheDestination(t *testing.T) {
	dest, _ := destPair(t)
	d := openTestDest(t, dest)

	first, err := d.createTemp("croc-stdin-")
	if err != nil {
		t.Fatalf("createTemp: %v", err)
	}
	second, err := d.createTemp("croc-stdin-")
	if err != nil {
		t.Fatalf("createTemp: %v", err)
	}
	if first == second {
		t.Error("createTemp returned the same name twice")
	}
	for _, name := range []string{first, second} {
		if strings.ContainsAny(name, "/\\") {
			t.Errorf("createTemp returned a path, not a name: %q", name)
		}
		if _, err := os.Lstat(filepath.Join(dest, name)); err != nil {
			t.Errorf("createTemp did not create %s: %v", name, err)
		}
	}
}

func TestIsEmptyDir(t *testing.T) {
	dest, _ := destPair(t)
	if err := os.MkdirAll(filepath.Join(dest, "empty"), 0o755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dest, "full"), 0o755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "full", "x"), nil, 0o644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	d := openTestDest(t, dest)
	for rel, want := range map[string]bool{"empty": true, "full": false} {
		got, err := d.isEmptyDir(rel)
		if err != nil {
			t.Fatalf("isEmptyDir(%s): %v", rel, err)
		}
		if got != want {
			t.Errorf("isEmptyDir(%s) = %v, want %v", rel, got, want)
		}
	}
}
