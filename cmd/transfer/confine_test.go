package transfer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
		// forward steps. these stay inside by their letters, so the lexical
		// walk alone accepted them, and os.Root created the link.
		{name: "symlink walking forward through an escaping symlink on disk", wantErr: true, wantErrContains: "cannot be resolved inside",
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "./", Name: "link", Symlink: "esc/x"}}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.Symlink(outside, filepath.Join(dest, "esc")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},
		{name: "symlink to an escaping symlink on disk", wantErr: true, wantErrContains: "cannot be resolved inside",
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "./", Name: "link", Symlink: "esc"}}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.Symlink(outside, filepath.Join(dest, "esc")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},
		{name: "symlink walking forward through an escaping symlink deeper down", wantErr: true, wantErrContains: "cannot be resolved inside",
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "sub/", Name: "link", Symlink: "../real/esc/x"}}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.MkdirAll(filepath.Join(dest, "real"), 0o755); err != nil {
					t.Fatalf("seeding dir: %v", err)
				}
				if err := os.Symlink(outside, filepath.Join(dest, "real", "esc")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},
		{name: "symlink walking forward through an in-tree symlink on disk",
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "./", Name: "link", Symlink: "sub/x"}}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.MkdirAll(filepath.Join(dest, "real"), 0o755); err != nil {
					t.Fatalf("seeding dir: %v", err)
				}
				if err := os.Symlink("real", filepath.Join(dest, "sub")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},
		// the declared link does not exist yet, so what lies under it cannot be
		// checked on disk.
		{name: "symlink walking forward through a declared symlink", wantErr: true, wantErrContains: "resolution crosses",
			info: SenderInfo{FilesToTransfer: []FileInfo{
				{FolderRemote: "./", Name: "sub", Symlink: "target"},
				{FolderRemote: "./", Name: "pwn", Symlink: "sub/esc"},
			}}},
		{name: "chain of declared symlinks", info: SenderInfo{FilesToTransfer: []FileInfo{
			{FolderRemote: "./", Name: "lib.so", Symlink: "lib.so.1"},
			{FolderRemote: "./", Name: "lib.so.1", Symlink: "lib.so.1.2"},
			file("./", "lib.so.1.2"),
		}}},
		// the declared target replaces the escaping link on disk, so the old one
		// is not what the new link resolves to.
		{name: "symlink to a declared symlink replacing an escaping one",
			info: SenderInfo{FilesToTransfer: []FileInfo{
				{FolderRemote: "./", Name: "link", Symlink: "esc"},
				{FolderRemote: "./", Name: "esc", Symlink: "target"},
			}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.Symlink(outside, filepath.Join(dest, "esc")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},

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

		// the entry's own path. os.Root follows a final symlink when a file is
		// opened by name, so these used to fail at the write, mid-transfer.
		{name: "file over an escaping symlink already on disk", wantErr: true, wantErrContains: "as a symlink",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "a.txt")}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.Symlink(filepath.Join(outside, "x"), filepath.Join(dest, "a.txt")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},
		{name: "file over an in-tree symlink already on disk", wantErr: true, wantErrContains: "as a symlink",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "a.txt")}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.WriteFile(filepath.Join(dest, "other.txt"), []byte("mine"), 0o644); err != nil {
					t.Fatalf("seeding file: %v", err)
				}
				if err := os.Symlink("other.txt", filepath.Join(dest, "a.txt")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},
		{name: "file over an existing directory", wantErr: true, wantErrContains: "already exists as a directory",
			info: SenderInfo{FilesToTransfer: []FileInfo{file("./", "a.txt")}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.MkdirAll(filepath.Join(dest, "a.txt"), 0o755); err != nil {
					t.Fatalf("seeding dir: %v", err)
				}
			}},
		{name: "symlink over an existing directory", wantErr: true, wantErrContains: "already exists as a directory",
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "./", Name: "link", Symlink: "target"}}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.MkdirAll(filepath.Join(dest, "link"), 0o755); err != nil {
					t.Fatalf("seeding dir: %v", err)
				}
			}},
		{name: "empty folder over an existing file", wantErr: true, wantErrContains: "already exists as a file",
			info: SenderInfo{EmptyFoldersToTransfer: []FileInfo{{FolderRemote: "empty/"}}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.WriteFile(filepath.Join(dest, "empty"), []byte("no"), 0o644); err != nil {
					t.Fatalf("seeding file: %v", err)
				}
			}},
		{name: "empty folder over an escaping symlink", wantErr: true, wantErrContains: "outside",
			info: SenderInfo{EmptyFoldersToTransfer: []FileInfo{{FolderRemote: "empty/"}}},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.Symlink(outside, filepath.Join(dest, "empty")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
			}},
		// what a repeated receive of the same tree finds, which has to keep
		// working: a file over a file, a link over a link, a folder over a folder.
		{name: "entries over their own earlier copies",
			info: SenderInfo{
				FilesToTransfer: []FileInfo{
					file("./", "a.txt"),
					{FolderRemote: "./", Name: "link", Symlink: "a.txt"},
				},
				EmptyFoldersToTransfer: []FileInfo{{FolderRemote: "empty/"}},
			},
			setup: func(t *testing.T, dest, outside string) {
				if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("old"), 0o644); err != nil {
					t.Fatalf("seeding file: %v", err)
				}
				if err := os.Symlink("a.txt", filepath.Join(dest, "link")); err != nil {
					t.Fatalf("seeding symlink: %v", err)
				}
				if err := os.MkdirAll(filepath.Join(dest, "empty"), 0o755); err != nil {
					t.Fatalf("seeding dir: %v", err)
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
		// a ".." after a forward step is refused whatever the names are, so the
		// case variant no longer decides this one; the fold cases further down
		// are the shapes where only the key can catch the alias. the reason
		// varies: a folding filesystem reports the alias, any other the shape.
		{name: "traverse out through a case variant of a declared symlink", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{
				{FolderRemote: "./", Name: "sub", Symlink: "target"},
				{FolderRemote: "./", Name: "pwn", Symlink: "SUB/x/../../outside"},
			}}},
		// the escape strings.ToLower let through: ſ (U+017F) folds to s under
		// unicode case folding, so "d/s" is "d/ſ" on APFS, and "s/../secret"
		// resolved to the parent of the destination.
		{name: "step back through a long-s alias of a declared symlink", wantErr: true,
			info: SenderInfo{FilesToTransfer: []FileInfo{
				{FolderRemote: "d/", Name: "ſ", Symlink: ".."},
				{FolderRemote: "d/", Name: "pwn", Symlink: "s/../secret"},
			}}},
		{name: "step back through a directory the target entered", wantErr: true, wantErrContains: "steps back",
			info: SenderInfo{FilesToTransfer: []FileInfo{{FolderRemote: "./", Name: "link", Symlink: "sub/../x"}}}},
		// a leading ".." is the shape every relative link has, and stays allowed.
		{name: "symlink stepping back then forward", info: SenderInfo{FilesToTransfer: []FileInfo{
			{FolderRemote: "bin/", Name: "tool", Symlink: "../lib/tool"},
			file("lib/", "tool"),
		}}},
		{name: "symlink with a dot-slash target", info: SenderInfo{FilesToTransfer: []FileInfo{
			{FolderRemote: "./", Name: "link", Symlink: "./target.txt"},
			file("./", "target.txt"),
		}}},
		// the same aliases where the link's own parent, not its target, is the
		// other spelling of a declared symlink. only the key sees these, so the
		// expectation follows the filesystem, as the case variants above do.
		{name: "write under a long-s alias of a declared symlink", wantErr: folds, wantErrContains: "also as a directory",
			info: SenderInfo{FilesToTransfer: []FileInfo{
				{FolderRemote: "d/", Name: "ſ", Symlink: ".."},
				file("d/s/", "x"),
			}}},
		{name: "symlink under a final-sigma alias of a declared symlink", wantErr: folds, wantErrContains: "also as a directory",
			info: SenderInfo{FilesToTransfer: []FileInfo{
				{FolderRemote: "d/", Name: "ς", Symlink: ".."},
				{FolderRemote: "d/σ/", Name: "link", Symlink: "../x"},
			}}},
		{name: "write under an nfd alias of a declared symlink", wantErr: folds, wantErrContains: "also as a directory",
			info: SenderInfo{FilesToTransfer: []FileInfo{
				{FolderRemote: "d/", Name: "\u00e9", Symlink: ".."},
				file("d/e\u0301/", "x"),
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
		// the archive is read while its entries are written, so an entry at
		// its own path truncated the source halfway through.
		{name: "entry named after the archive", entries: []string{"payload.zip"}, wantErr: true},
		{name: "entry that is a case variant of the archive's name", entries: []string{"PAYLOAD.ZIP"}, wantErr: true},
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

			paths, err := validateArchive(root, r.File, "payload.zip")
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
	// names with no ascii letter, or with a letter whose case does not round
	// trip, used to decide the answer for the whole directory: "odd" has
	// nothing to swap and must fall back to the write probe, and the dotless
	// i in "turkic" must be left alone so ".txt" carries the probe.
	for dir, names := range map[string][]string{
		"odd":    {"ß", "ı"},
		"turkic": {"ı.txt"},
	} {
		if err := os.MkdirAll(filepath.Join(dest, dir), 0o755); err != nil {
			t.Fatalf("creating dir: %v", err)
		}
		for _, name := range names {
			if err := os.WriteFile(filepath.Join(dest, dir, name), nil, 0o600); err != nil {
				t.Fatalf("seeding %s: %v", name, err)
			}
		}
	}

	root, err := os.OpenRoot(dest)
	if err != nil {
		t.Fatalf("opening destination: %v", err)
	}
	defer root.Close()

	for _, dir := range []string{"populated", "empty", "odd", "turkic", ""} {
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
	for dir, want := range map[string]int{"empty": 0, "odd": 2} {
		entries, err := os.ReadDir(filepath.Join(dest, dir))
		if err != nil {
			t.Fatalf("reading %s: %v", dir, err)
		}
		if len(entries) != want {
			t.Errorf("probe left residue in %s: %d entries, want %d", dir, len(entries), want)
		}
	}
}

// TestFoldKey pins the aliases strings.ToLower missed. Each pair is one name
// on a default APFS volume, and the first three carried a working escape.
func TestFoldKey(t *testing.T) {
	for _, pair := range [][2]string{
		{"ſ", "s"},            // long s, U+017F
		{"ς", "σ"},            // final sigma, U+03C2
		{"\u00e9", "e\u0301"}, // é precomposed and decomposed
		{"\u212a", "k"},       // kelvin sign
		{"A.txt", "a.txt"},
		{"d/ſ", "d/s"},
	} {
		if foldKey(pair[0]) != foldKey(pair[1]) {
			t.Errorf("foldKey(%q) = %q and foldKey(%q) = %q, want equal", pair[0], foldKey(pair[0]), pair[1], foldKey(pair[1]))
		}
	}
	for _, pair := range [][2]string{
		{"a", "b"},
		{"ı", "i"}, // the dotless i is its own letter outside turkic locales
		{"d/s", "ds"},
	} {
		if foldKey(pair[0]) == foldKey(pair[1]) {
			t.Errorf("foldKey(%q) = foldKey(%q) = %q, want distinct", pair[0], pair[1], foldKey(pair[0]))
		}
	}
}

func TestSwapASCIICase(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		ok         bool
	}{
		{"abc", "ABC", true},
		{"Readable", "rEADABLE", true},
		{"ı.txt", "ı.TXT", true}, // the dotless i is left alone
		{"Ünï", "ÜNï", true},     // only the ascii letter moves
		{"ß", "ß", false},        // nothing ascii to swap
		{"123", "123", false},
		{"", "", false},
	} {
		got, ok := swapASCIICase(tc.name)
		if got != tc.want || ok != tc.ok {
			t.Errorf("swapASCIICase(%q) = %q, %v, want %q, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// caseVariants returns n distinct spellings of one name that differ only by
// case, each declaring the same content so no conflict rule refuses them.
func caseVariants(folder string, n int) []FileInfo {
	const base = "abcdefghijklmnop"
	out := make([]FileInfo, 0, n)
	for i := 0; i < n; i++ {
		b := []byte(base)
		for bit := range b {
			if i&(1<<bit) != 0 {
				b[bit] -= 'a' - 'A'
			}
		}
		out = append(out, file(folder, string(b)))
	}
	return out
}

// TestValidateManifestBoundsSpellings: a sender cannot make validation
// quadratic by declaring one name in thousands of case variants. before the
// bound, 4000 of them took 16s on APFS; now the manifest is refused at the
// first spelling past the limit, whatever the filesystem.
func TestValidateManifestBoundsSpellings(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatalf("opening destination: %v", err)
	}
	defer root.Close()

	start := time.Now()
	_, _, err = validateManifest(root, SenderInfo{FilesToTransfer: caseVariants("./", 4000)})
	if err == nil || !strings.Contains(err.Error(), "spellings") {
		t.Fatalf("validateManifest = %v, want the spellings refusal", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("refusing 4000 variants took %v", elapsed)
	}

	// the same bound applies to a directory spelled many ways.
	var viaDirs []FileInfo
	for i, f := range caseVariants("./", maxSpellings+1) {
		viaDirs = append(viaDirs, file(f.Name+"/", fmt.Sprintf("f%d", i)))
	}
	if _, _, err := validateManifest(root, SenderInfo{FilesToTransfer: viaDirs}); err == nil || !strings.Contains(err.Error(), "spellings") {
		t.Errorf("directory variants: validateManifest = %v, want the spellings refusal", err)
	}
}

// TestValidateManifestAllowsSpellingsUpToTheBound: up to maxSpellings variants
// of identical content are still the harmless repeat they were, and exact
// repeats of one name never count against the bound.
func TestValidateManifestAllowsSpellingsUpToTheBound(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatalf("opening destination: %v", err)
	}
	defer root.Close()

	if _, _, err := validateManifest(root, SenderInfo{FilesToTransfer: caseVariants("./", maxSpellings)}); err != nil {
		t.Errorf("%d variants refused: %v", maxSpellings, err)
	}
	repeats := make([]FileInfo, 5000)
	for i := range repeats {
		repeats[i] = file("./", "a.txt")
	}
	if _, _, err := validateManifest(root, SenderInfo{FilesToTransfer: repeats}); err != nil {
		t.Errorf("exact repeats refused: %v", err)
	}
}
