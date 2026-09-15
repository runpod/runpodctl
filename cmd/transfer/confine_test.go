package transfer

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// destFolds measures whether the test filesystem aliases names by case, so the
// expectations below track the actual filesystem instead of guessing from
// GOOS. macOS's default APFS folds, Linux's ext4 usually does not, and ext4
// can fold per directory.
func destFolds(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe"), nil, 0o600); err != nil {
		t.Fatalf("writing probe: %v", err)
	}
	lower, err := os.Lstat(filepath.Join(dir, "probe"))
	if err != nil {
		t.Fatalf("stat probe: %v", err)
	}
	upper, err := os.Lstat(filepath.Join(dir, "PROBE"))
	if err != nil {
		return false
	}
	return os.SameFile(lower, upper)
}

func TestNormalizeRel(t *testing.T) {
	for _, tc := range []struct {
		declared string
		want     string
		wantErr  bool
	}{
		{declared: "./", want: ""},
		{declared: ".", want: ""},
		{declared: "", want: ""},
		{declared: "a.txt", want: "a.txt"},
		{declared: "sub/", want: "sub"},
		{declared: "a//b/./c", want: "a/b/c"},
		// a ".." that is part of a name, not a component, is legitimate. the
		// old check was strings.Contains(path, "..") and rejected these.
		{declared: "file..txt", want: "file..txt"},
		{declared: "..hidden", want: "..hidden"},
		{declared: "a..b/c", want: "a..b/c"},

		{declared: "../x", wantErr: true},
		{declared: "a/../../x", wantErr: true},
		{declared: "..", wantErr: true},
		{declared: "/etc/passwd", wantErr: true},
		{declared: "//srv/share", wantErr: true},
		{declared: `a\b`, wantErr: true},
		{declared: "a\x00b", wantErr: true},
	} {
		got, err := normalizeRel(tc.declared)
		switch {
		case tc.wantErr && err == nil:
			t.Errorf("normalizeRel(%q) = %q, want a refusal", tc.declared, got)
		case !tc.wantErr && err != nil:
			t.Errorf("normalizeRel(%q) refused: %v", tc.declared, err)
		case !tc.wantErr && got != tc.want:
			t.Errorf("normalizeRel(%q) = %q, want %q", tc.declared, got, tc.want)
		}
	}
}

// file builds a manifest entry with content that matches any other entry built
// the same way, so duplicate cases can vary one field at a time.
func file(folder, name string) FileInfo {
	return FileInfo{FolderRemote: folder, Name: name, Size: 4, Hash: []byte{1, 2, 3, 4}}
}

func TestValidateManifest(t *testing.T) {
	folds := destFolds(t)

	for _, tc := range []struct {
		name string
		info SenderInfo
		// setup prepares the destination and a sibling directory outside it.
		setup   func(t *testing.T, dest, outside string)
		wantErr bool
		// wantErrContains is checked only when a refusal is expected.
		wantErrContains string
	}{
		{name: "empty manifest", info: SenderInfo{}},
		{name: "plain file", info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "a.txt")}}},
		{name: "nested file", info: SenderInfo{FilesToTransfer: []FileInfo{file("dir/sub/", "a.txt")}}},
		{name: "empty folder", info: SenderInfo{EmptyFoldersToTransfer: []FileInfo{{FolderRemote: "dir/empty/"}}}},

		{name: "traversal in folder", wantErr: true, wantErrContains: "traverses outside",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("../", "pwn")}}},
		{name: "traversal deeper in folder", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{file("a/../../", "pwn")}}},
		{name: "traversal in name", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "../pwn")}}},
		{name: "absolute folder", wantErr: true, wantErrContains: "absolute",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("/tmp/", "pwn")}}},
		{name: "absolute name", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "/etc/passwd")}}},
		{name: "unc folder", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{file("//srv/share/", "pwn")}}},
		{name: "backslash", wantErr: true, wantErrContains: "backslash",
			info: SenderInfo{FilesToTransfer: []FileInfo{file(`..\`, "pwn")}}},
		{name: "nul byte", wantErr: true, wantErrContains: "nul byte",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "a\x00b")}}},
		{name: "name carrying a separator", wantErr: true, wantErrContains: "single path component",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "sub/a.txt")}}},
		// on unix "c:" is an ordinary directory name and VolumeName returns "";
		// on windows it is a volume and is refused. os.Root confines it either way.
		{name: "drive letter", wantErr: runtime.GOOS == "windows",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("c:/", "pwn")}}},

		{name: "negative size", wantErr: true, wantErrContains: "negative size",
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "./", Name: "a", Size: -1}}}},
		{name: "archive that is also a symlink", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "./", Name: "a", Size: 5, TempFile: true, Symlink: "x"}}}},
		{name: "zero length archive", wantErr: true, wantErrContains: "zero size",
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "./", Name: "a.zip", TempFile: true}}}},
		{name: "unsupported hash algorithm", wantErr: true, wantErrContains: "hash algorithm",
			info: SenderInfo{HashAlgorithm: "sha1", FilesToTransfer: []FileInfo{file("./", "a")}}},
		{name: "negative folder count", wantErr: true,
			info: SenderInfo{TotalNumberFolders: -1}},

		// `send a.txt a.txt` produces two identical entries. GetFilesInfo never
		// deduped and the old receiver overwrote identical content harmlessly,
		// so a blanket duplicate refusal would break a working invocation.
		{name: "same file sent twice", info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "a.txt"), file("./", "a.txt")}}},
		{name: "duplicate with a different hash", wantErr: true, wantErrContains: "different contents",
			info: SenderInfo{FilesToTransfer: []FileInfo{
				file("./", "a.txt"),
				{FolderRemote: "./", Name: "a.txt", Size: 4, Hash: []byte{9, 9, 9, 9}},
			}}},
		{name: "duplicate with a different size", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{
				file("./", "a.txt"),
				{FolderRemote: "./", Name: "a.txt", Size: 7, Hash: []byte{1, 2, 3, 4}},
			}}},

		{name: "file then the same path as a directory", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "a"), file("a/", "b")}}},
		{name: "directory then the same path as a file", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{file("a/", "b"), file("./", "a")}}},
		{name: "file colliding with an empty folder", wantErr: true,
			info: SenderInfo{
				FilesToTransfer:        []FileInfo{file("./", "a")},
				EmptyFoldersToTransfer: []FileInfo{{FolderRemote: "a/"}},
			}},

		{name: "symlink pointing inside", info: SenderInfo{FilesToTransfer: []FileInfo{
			{FolderRemote: "./", Name: "link", Symlink: "target.txt"},
			file("./", "target.txt"),
		}}},
		{name: "symlink pointing sideways within the tree", info: SenderInfo{FilesToTransfer: []FileInfo{
			{FolderRemote: "sub/", Name: "link", Symlink: "../other/x"},
		}}},
		{name: "symlink with an absolute target", wantErr: true, wantErrContains: "absolute path",
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "./", Name: "link", Symlink: "/etc/passwd"}}}},
		// os.Root creates this link happily and merely refuses to follow it, so
		// without prevalidation the receive leaves an escaping link behind for
		// whatever walks the tree next.
		{name: "symlink escaping via dotdot", wantErr: true, wantErrContains: "points outside",
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "./", Name: "link", Symlink: "../outside"}}}},
		{name: "symlink escaping from a subdirectory", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "sub/", Name: "link", Symlink: "../../outside"}}}},
		{name: "write through a declared symlink", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{
				{FolderRemote: "./", Name: "sub", Symlink: "target"},
				file("sub/", "x"),
			}}},
		{name: "write through a declared symlink, declared second", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{
				file("sub/", "x"),
				{FolderRemote: "./", Name: "sub", Symlink: "target"},
			}}},
		// the lexical ".." in the second target is only sound if "sub" is a real
		// directory, and this manifest says it is not.
		{name: "traverse out through a declared symlink", wantErr: true, wantErrContains: "resolution crosses",
			info: SenderInfo{FilesToTransfer: []FileInfo{
				{FolderRemote: "./", Name: "sub", Symlink: "target"},
				{FolderRemote: "./", Name: "pwn", Symlink: "sub/x/../../outside"},
			}}},

		{name: "ancestor is an escaping symlink already on disk", wantErr: true, wantErrContains: "outside",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("sub/", "x")}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.Symlink(outside, filepath.Join(dest, "sub")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},
		{name: "ancestor is an in-tree symlink already on disk",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("sub/", "x")}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.MkdirAll(filepath.Join(dest, "real"), 0o755); err != nil {
					t.Fatalf("seeding dir: %v", err)
				}
				if err := os.Symlink("real", filepath.Join(dest, "sub")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},
		{name: "ancestor is an existing file", wantErr: true, wantErrContains: "already exists as a file",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("sub/", "x")}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.WriteFile(filepath.Join(dest, "sub"), []byte("no"), 0o644); err != nil {
					t.Fatalf("seeding file: %v", err)
				}
			}},
		{name: "ancestor is a dangling symlink", wantErr: true, wantErrContains: "no target",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("sub/", "x")}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.Symlink("nowhere", filepath.Join(dest, "sub")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},

		// whether two spellings are one destination is a filesystem property,
		// so these expectations are measured, never hardcoded by GOOS.
		{name: "case variants with different content", wantErr: folds,
			info: SenderInfo{FilesToTransfer: []FileInfo{
				file("./", "A.txt"),
				{FolderRemote: "./", Name: "a.txt", Size: 9, Hash: []byte{9}},
			}}},
		{name: "case variants with identical content",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "A.txt"), file("./", "a.txt")}}},
		{name: "case variant file against a directory prefix", wantErr: folds,
			info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "A"), file("a/", "b")}}},
		{name: "traverse out through a case variant of a declared symlink", wantErr: folds,
			info: SenderInfo{FilesToTransfer: []FileInfo{
				{FolderRemote: "./", Name: "sub", Symlink: "target"},
				{FolderRemote: "./", Name: "pwn", Symlink: "SUB/x/../../outside"},
			}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			dest := filepath.Join(base, "dest")
			outside := filepath.Join(base, "outside")
			for _, dir := range []string{dest, outside} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("creating %s: %v", dir, err)
				}
			}
			if tc.setup != nil {
				tc.setup(t, dest, outside)
			}

			root, err := os.OpenRoot(dest)
			if err != nil {
				t.Fatalf("opening destination: %v", err)
			}
			defer root.Close()

			filePaths, folderPaths, err := validateManifest(root, tc.info)
			switch {
			case tc.wantErr && err == nil:
				t.Fatalf("manifest accepted, want a refusal (paths %v %v)", filePaths, folderPaths)
			case !tc.wantErr && err != nil:
				t.Fatalf("manifest refused: %v", err)
			}
			if tc.wantErr {
				if tc.wantErrContains != "" && !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Errorf("refusal %q does not mention %q", err, tc.wantErrContains)
				}
				return
			}

			// on success the returned paths must line up with the manifest
			// positionally: the wire protocol indexes by position, so renumbering
			// would silently retarget a later write.
			if len(filePaths) != len(tc.info.FilesToTransfer) {
				t.Errorf("got %d file paths, want %d", len(filePaths), len(tc.info.FilesToTransfer))
			}
			if len(folderPaths) != len(tc.info.EmptyFoldersToTransfer) {
				t.Errorf("got %d folder paths, want %d", len(folderPaths), len(tc.info.EmptyFoldersToTransfer))
			}
		})
	}
}

func TestValidateManifestReturnsPositionalPaths(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatalf("opening destination: %v", err)
	}
	defer root.Close()

	info := SenderInfo{
		FilesToTransfer: []FileInfo{
			file("./", "top.txt"),
			file("dir/sub/", "deep.txt"),
			file("./", "top.txt"), // the `send a.txt a.txt` shape
		},
		EmptyFoldersToTransfer: []FileInfo{{FolderRemote: "dir/empty/"}},
	}

	filePaths, folderPaths, err := validateManifest(root, info)
	if err != nil {
		t.Fatalf("validateManifest: %v", err)
	}
	// index 2 keeps its own slot rather than being folded into index 0.
	want := []string{"top.txt", "dir/sub/deep.txt", "top.txt"}
	for i, w := range want {
		if filePaths[i] != w {
			t.Errorf("filePaths[%d] = %q, want %q", i, filePaths[i], w)
		}
	}
	if len(folderPaths) != 1 || folderPaths[0] != "dir/empty" {
		t.Errorf("folderPaths = %v, want [dir/empty]", folderPaths)
	}
}

// TestValidateManifestWritesNothing covers the all-or-nothing contract: the
// case-folding probe is the only write the validator can make, and a refused
// manifest must leave the destination exactly as it was.
func TestValidateManifestWritesNothing(t *testing.T) {
	if !destFolds(t) {
		t.Skip("filesystem does not fold case, so the write probe is not reached")
	}
	dest := t.TempDir()
	root, err := os.OpenRoot(dest)
	if err != nil {
		t.Fatalf("opening destination: %v", err)
	}
	defer root.Close()

	// a folded collision in an empty directory is what forces the probe of last
	// resort, which has to create a file to get its answer.
	if _, _, err := validateManifest(root, SenderInfo{FilesToTransfer: []FileInfo{
		file("./", "A.txt"),
		{FolderRemote: "./", Name: "a.txt", Size: 9, Hash: []byte{9}},
	}}); err == nil {
		t.Fatal("colliding manifest accepted")
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("destination is not empty after a refusal: %v", names)
	}
}

func TestValidateArchive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dirs    []string
		entries []string
		wantErr bool
	}{
		{name: "ordinary archive", dirs: []string{"pkg/"}, entries: []string{"pkg/a.txt", "pkg/sub/b.txt"}},
		{name: "identical duplicate", entries: []string{"a.txt", "a.txt"}},
		{name: "traversal", entries: []string{"../escape.txt"}, wantErr: true},
		{name: "traversal below a directory", entries: []string{"pkg/../../escape"}, wantErr: true},
		{name: "absolute", entries: []string{"/etc/passwd"}, wantErr: true},
		{name: "backslash", entries: []string{`..\escape`}, wantErr: true},
		{name: "file that is also a directory prefix", entries: []string{"a", "a/b"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := zip.NewWriter(&buf)
			for _, dir := range tc.dirs {
				if _, err := w.Create(dir); err != nil {
					t.Fatalf("adding %s: %v", dir, err)
				}
			}
			for _, name := range tc.entries {
				f, err := w.Create(name)
				if err != nil {
					t.Fatalf("adding %s: %v", name, err)
				}
				if _, err := f.Write([]byte("same")); err != nil {
					t.Fatalf("writing %s: %v", name, err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatalf("closing archive: %v", err)
			}
			r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			if err != nil {
				t.Fatalf("reading archive: %v", err)
			}

			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatalf("opening destination: %v", err)
			}
			defer root.Close()

			paths, err := validateArchive(root, r.File)
			switch {
			case tc.wantErr && err == nil:
				t.Fatalf("archive accepted, want a refusal (paths %v)", paths)
			case !tc.wantErr && err != nil:
				t.Fatalf("archive refused: %v", err)
			case err == nil && len(paths) != len(r.File):
				t.Errorf("got %d paths for %d entries", len(paths), len(r.File))
			}
		})
	}
}

// TestFoldProbeMatchesFilesystem checks the probe against ground truth, both on
// a directory with something to read and on an empty one, where it has to fall
// back to writing.
func TestFoldProbeMatchesFilesystem(t *testing.T) {
	want := destFolds(t)
	dest := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dest, "populated"), 0o755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "populated", "Readable"), nil, 0o600); err != nil {
		t.Fatalf("seeding file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dest, "empty"), 0o755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}

	root, err := os.OpenRoot(dest)
	if err != nil {
		t.Fatalf("opening destination: %v", err)
	}
	defer root.Close()

	for _, dir := range []string{"populated", "empty", ""} {
		probe := newFoldProbe(root)
		got, err := probe.insensitive(dir)
		if err != nil {
			t.Fatalf("probing %q: %v", dir, err)
		}
		if got != want {
			t.Errorf("probe of %q = %v, want %v", dir, got, want)
		}
	}

	// the probe of last resort must clean up after itself.
	entries, err := os.ReadDir(filepath.Join(dest, "empty"))
	if err != nil {
		t.Fatalf("reading empty dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("probe left residue: %d entries", len(entries))
	}
}
