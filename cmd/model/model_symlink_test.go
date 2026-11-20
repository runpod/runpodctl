package model

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/runpod/runpodctl/api"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func collectedNames(files []modelFile) []string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.RelativePath
	}
	return names
}

func TestCollectModelFiles_SymlinkInsideRootAllowed(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "real", "weights.bin"), "weights")
	symlinkOrSkip(t, filepath.Join("real", "weights.bin"), filepath.Join(root, "alias.bin"))

	files, err := collectModelFiles(root)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if got := strings.Join(collectedNames(files), ","); got != "alias.bin,real/weights.bin" {
		t.Fatalf("unexpected files: %s", got)
	}
	alias := files[0]
	if alias.Size != int64(len("weights")) {
		t.Fatalf("alias size = %d", alias.Size)
	}
	if filepath.Base(alias.ResolvedPath) != "weights.bin" || alias.ResolvedPath == alias.AbsolutePath {
		t.Fatalf("symlink should resolve to its target, got abs=%s resolved=%s", alias.AbsolutePath, alias.ResolvedPath)
	}
	if alias.uploadPath() != alias.ResolvedPath {
		t.Fatal("upload must open the resolved path")
	}
}

func TestCollectModelFiles_AbsoluteSymlinkOutsideRootRefused(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeTestFile(t, outside, "secret")
	writeTestFile(t, filepath.Join(root, "ok.bin"), "ok")
	symlinkOrSkip(t, outside, filepath.Join(root, "leak.bin"))

	_, err := collectModelFiles(root)
	if err == nil {
		t.Fatal("expected a symlink to a file outside --model-path to be refused")
	}
	if !strings.Contains(err.Error(), "outside the model directory") || !strings.Contains(err.Error(), "leak.bin") {
		t.Fatalf("error should name the offending link and the reason: %v", err)
	}
	if !errors.Is(err, errSymlinkOutsideModelPath) {
		t.Fatalf("error should wrap errSymlinkOutsideModelPath: %v", err)
	}
	// the message must tell the user how to fix it, not just that it was refused.
	for _, want := range []string{"cp -rL", "rsync -rL", "--model-path", "models--<org>--<name>/snapshots/<rev>", "huggingface-cli download --local-dir", filepath.Base(outside)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}
}

func TestCollectModelFiles_RelativeEscapeRefused(t *testing.T) {
	parent := t.TempDir()
	writeTestFile(t, filepath.Join(parent, "secret.txt"), "secret")
	root := filepath.Join(parent, "model")
	writeTestFile(t, filepath.Join(root, "ok.bin"), "ok")
	symlinkOrSkip(t, filepath.Join("..", "..", "secret.txt"), filepath.Join(root, "sub", "escape.bin"))

	if _, err := collectModelFiles(root); err == nil || !strings.Contains(err.Error(), "outside the model directory") {
		t.Fatalf("expected ../ escape to be refused, got %v", err)
	}
}

func TestCollectModelFiles_SiblingWithSharedPrefixRefused(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "model")
	writeTestFile(t, filepath.Join(root, "ok.bin"), "ok")
	writeTestFile(t, filepath.Join(parent, "model-private", "secret.txt"), "secret")
	symlinkOrSkip(t, filepath.Join("..", "..", "model-private", "secret.txt"), filepath.Join(root, "sub", "link.bin"))

	if _, err := collectModelFiles(root); err == nil {
		t.Fatal("a sibling directory that merely shares the root's name prefix is not inside the root")
	}
}

func TestCollectModelFiles_RelativeModelPath(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "model")
	writeTestFile(t, filepath.Join(root, "ok.bin"), "ok")
	t.Chdir(parent)

	files, err := collectModelFiles("model")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(files) != 1 || files[0].RelativePath != "ok.bin" || !filepath.IsAbs(files[0].AbsolutePath) {
		t.Fatalf("unexpected result: %+v", files)
	}
}

func TestCollectModelFiles_ModelPathItselfASymlink(t *testing.T) {
	parent := t.TempDir()
	writeTestFile(t, filepath.Join(parent, "real", "ok.bin"), "ok")
	symlinkOrSkip(t, "real", filepath.Join(parent, "link"))

	files, err := collectModelFiles(filepath.Join(parent, "link"))
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(files) != 1 || files[0].RelativePath != "ok.bin" {
		t.Fatalf("unexpected result: %+v", files)
	}
}

func TestCollectModelFiles_SymlinkedDirectoryRefused(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "ok.bin"), "ok")
	inside := filepath.Join(root, "dir")
	writeTestFile(t, filepath.Join(inside, "a.bin"), "a")
	symlinkOrSkip(t, "dir", filepath.Join(root, "dirlink"))

	if _, err := collectModelFiles(root); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("expected a symlinked directory to be refused, got %v", err)
	}
}

func TestCollectModelFiles_BrokenSymlinkRefused(t *testing.T) {
	root := t.TempDir()
	symlinkOrSkip(t, "missing.bin", filepath.Join(root, "broken.bin"))

	if _, err := collectModelFiles(root); err == nil {
		t.Fatal("expected a dangling symlink to be refused")
	}
}

// the Hugging Face cache stores every snapshot file as a relative symlink into
// the repo's blobs/ directory; that layout must keep working.
func TestCollectModelFiles_HuggingFaceSnapshotLayout(t *testing.T) {
	cache := t.TempDir()
	repo := filepath.Join(cache, "models--owner--name")
	writeTestFile(t, filepath.Join(repo, "blobs", "sha-config"), "{}")
	writeTestFile(t, filepath.Join(repo, "blobs", "sha-weights"), "weights")
	snapshot := filepath.Join(repo, "snapshots", "abc123")
	symlinkOrSkip(t, filepath.Join("..", "..", "blobs", "sha-config"), filepath.Join(snapshot, "config.json"))
	symlinkOrSkip(t, filepath.Join("..", "..", "blobs", "sha-weights"), filepath.Join(snapshot, "sub", "model.safetensors"))
	// nested files sit one level deeper, so their relative link needs one more ../
	if err := os.Remove(filepath.Join(snapshot, "sub", "model.safetensors")); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, filepath.Join("..", "..", "..", "blobs", "sha-weights"), filepath.Join(snapshot, "sub", "model.safetensors"))

	files, err := collectModelFiles(snapshot)
	if err != nil {
		t.Fatalf("HF cache layout must be accepted: %v", err)
	}
	if got := strings.Join(collectedNames(files), ","); got != "config.json,sub/model.safetensors" {
		t.Fatalf("unexpected files: %s", got)
	}
	for _, f := range files {
		if !strings.Contains(f.ResolvedPath, string(filepath.Separator)+"blobs"+string(filepath.Separator)) {
			t.Errorf("%s should resolve into blobs/, got %s", f.RelativePath, f.ResolvedPath)
		}
		if strings.Contains(f.AbsolutePath, "blobs") {
			t.Errorf("AbsolutePath must stay the link (what delete-after-upload removes): %s", f.AbsolutePath)
		}
	}
}

func TestCollectModelFiles_HuggingFaceLayoutDoesNotWidenTrustBeyondRepo(t *testing.T) {
	cache := t.TempDir()
	repo := filepath.Join(cache, "models--owner--name")
	writeTestFile(t, filepath.Join(cache, "models--other--repo", "blobs", "sha"), "other")
	writeTestFile(t, filepath.Join(cache, "secret.txt"), "secret")
	snapshot := filepath.Join(repo, "snapshots", "abc123")
	writeTestFile(t, filepath.Join(snapshot, "ok.bin"), "ok")

	t.Run("other repo in the same cache", func(t *testing.T) {
		symlinkOrSkip(t, filepath.Join("..", "..", "..", "models--other--repo", "blobs", "sha"), filepath.Join(snapshot, "other.bin"))
		if _, err := collectModelFiles(snapshot); err == nil {
			t.Fatal("a link into another repo's blobs must be refused")
		}
		if err := os.Remove(filepath.Join(snapshot, "other.bin")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("refs and other revisions of the same repo", func(t *testing.T) {
		writeTestFile(t, filepath.Join(repo, "refs", "main"), "abc123")
		writeTestFile(t, filepath.Join(repo, "snapshots", "other-rev", "private.bin"), "other revision")
		for name, target := range map[string]string{
			"refs.bin":  filepath.Join("..", "..", "refs", "main"),
			"other.bin": filepath.Join("..", "other-rev", "private.bin"),
		} {
			link := filepath.Join(snapshot, name)
			symlinkOrSkip(t, target, link)
			_, err := collectModelFiles(snapshot)
			if err == nil || !errors.Is(err, errSymlinkOutsideModelPath) {
				t.Fatalf("a link to %s inside the repo but outside blobs/ must be refused, got %v", target, err)
			}
			if rmErr := os.Remove(link); rmErr != nil {
				t.Fatal(rmErr)
			}
		}
	})
	t.Run("cache root", func(t *testing.T) {
		symlinkOrSkip(t, filepath.Join("..", "..", "..", "secret.txt"), filepath.Join(snapshot, "secret.bin"))
		if _, err := collectModelFiles(snapshot); err == nil {
			t.Fatal("a link out of the repo dir must be refused")
		}
	})
}

func TestCollectModelFiles_BlobsLayoutOutsideHuggingFaceRepoRefused(t *testing.T) {
	// same shape as an HF snapshot, but the directories are not named like one.
	base := t.TempDir()
	writeTestFile(t, filepath.Join(base, "blobs", "sha"), "blob")
	root := filepath.Join(base, "snapshots", "rev")
	writeTestFile(t, filepath.Join(root, "ok.bin"), "ok")
	symlinkOrSkip(t, filepath.Join("..", "..", "blobs", "sha"), filepath.Join(root, "link.bin"))

	if _, err := collectModelFiles(root); err == nil {
		t.Fatal("the blobs allowance applies only to a models--<owner>--<name> repo directory")
	}
}

// newUnreachableUploadServer counts the requests it receives: completeModelUploadFile
// must refuse a path swapped after the scan before any request is made.
func newUnreachableUploadServer(t *testing.T) (server *httptest.Server, hits func() int) {
	t.Helper()
	var mu sync.Mutex
	count := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("drain request body: %v", err)
		}
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, func() int { mu.Lock(); defer mu.Unlock(); return count }
}

func uploadFor(serverURL string) *api.ModelRepoUpload {
	return &api.ModelRepoUpload{
		PartSizeBytes: 1 << 20,
		CompleteURL:   serverURL,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: serverURL}},
	}
}

func TestUpload_SwapToSymlinkAfterScanRefused(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "model.bin"), "original")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeTestFile(t, outside, "secret")

	files, err := collectModelFiles(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("collect: %v %+v", err, files)
	}
	file := files[0]

	// swap: the scanned file is replaced by a symlink to a file outside the root.
	if rmErr := os.Remove(file.ResolvedPath); rmErr != nil {
		t.Fatal(rmErr)
	}
	symlinkOrSkip(t, outside, file.ResolvedPath)

	server, hits := newUnreachableUploadServer(t)
	err = completeModelUploadFile(t.Context(), uploadFor(server.URL), file.uploadPath(), file.info, nil)
	if err == nil {
		t.Fatal("expected the swapped path to be refused")
	}
	if !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("unexpected error: %v", err)
	}
	if hits() != 0 {
		t.Fatalf("nothing may be sent for a refused file, server saw %d request(s)", hits())
	}
}

func TestUpload_SwapToDifferentRegularFileAfterScanRefused(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "model.bin"), "original")

	files, err := collectModelFiles(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("collect: %v %+v", err, files)
	}
	file := files[0]
	// the replacement is created while the original still exists (so it gets a
	// different inode) and renamed over it, as an editor or attacker would.
	replacement := filepath.Join(root, "replacement.tmp")
	writeTestFile(t, replacement, "different")
	if renameErr := os.Rename(replacement, file.ResolvedPath); renameErr != nil {
		t.Fatal(renameErr)
	}

	server, hits := newUnreachableUploadServer(t)
	err = completeModelUploadFile(t.Context(), uploadFor(server.URL), file.uploadPath(), file.info, nil)
	if err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("expected the replaced file to be refused, got %v", err)
	}
	if hits() != 0 {
		t.Fatalf("server saw %d request(s)", hits())
	}
}

func TestUpload_SymlinkedHuggingFaceFileUploadsBlobContent(t *testing.T) {
	cache := t.TempDir()
	repo := filepath.Join(cache, "models--owner--name")
	writeTestFile(t, filepath.Join(repo, "blobs", "sha"), "blob-bytes")
	snapshot := filepath.Join(repo, "snapshots", "rev")
	symlinkOrSkip(t, filepath.Join("..", "..", "blobs", "sha"), filepath.Join(snapshot, "model.bin"))

	files, err := collectModelFiles(snapshot)
	if err != nil || len(files) != 1 {
		t.Fatalf("collect: %v %+v", err, files)
	}

	var mu sync.Mutex
	var uploaded string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("read request body: %v", readErr)
		}
		if r.Method == http.MethodPut {
			mu.Lock()
			uploaded = string(body)
			mu.Unlock()
		}
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	file := files[0]
	if err := completeModelUploadFile(t.Context(), uploadFor(server.URL), file.uploadPath(), file.info, nil); err != nil {
		t.Fatalf("upload: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if uploaded != "blob-bytes" {
		t.Fatalf("uploaded %q", uploaded)
	}
}

func TestOpenScannedModelFile_RefusesSymlinkWithoutScanInfo(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "real.bin"), "x")
	symlinkOrSkip(t, "real.bin", filepath.Join(root, "link.bin"))

	if f, _, err := openScannedModelFile(filepath.Join(root, "link.bin"), nil); err == nil {
		t.Cleanup(func() {
			if closeErr := f.Close(); closeErr != nil {
				t.Errorf("close: %v", closeErr)
			}
		})
		t.Fatal("the upload path must never be a symlink")
	}
	f, _, err := openScannedModelFile(filepath.Join(root, "real.bin"), nil)
	if err != nil {
		t.Fatalf("regular file should open: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// the scan-time FileInfo must have its identity loaded during the scan: on
// Windows os.Lstat loads it lazily on the first os.SameFile call, which would
// otherwise happen at upload time, after a swap.
func TestCollectModelFiles_PinsFileIdentityAtScanTime(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.bin"), "a")
	writeTestFile(t, filepath.Join(root, "b.bin"), "b")

	orig := pinScanFileIdentity
	t.Cleanup(func() { pinScanFileIdentity = orig })
	var pinned []os.FileInfo
	pinScanFileIdentity = func(info os.FileInfo) error {
		pinned = append(pinned, info)
		return orig(info)
	}

	files, err := collectModelFiles(root)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(pinned) != len(files) {
		t.Fatalf("expected one identity pin per scanned file, got %d for %d files", len(pinned), len(files))
	}
	for _, f := range files {
		found := false
		for _, p := range pinned {
			if p == f.info.(*scannedModelInfo).FileInfo {
				found = true
			}
		}
		if !found {
			t.Errorf("the FileInfo kept for %s was not the one pinned at scan time", f.RelativePath)
		}
	}
}

func TestCollectModelFiles_IdentityFailureAbortsScan(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.bin"), "a")

	orig := pinScanFileIdentity
	t.Cleanup(func() { pinScanFileIdentity = orig })
	pinScanFileIdentity = func(os.FileInfo) error { return errors.New("no file id") }

	if _, err := collectModelFiles(root); err == nil || !strings.Contains(err.Error(), "no file id") {
		t.Fatalf("expected the scan to fail when identity cannot be pinned, got %v", err)
	}
}

// swapDirForOutsideLink replaces dir with a symlink to a fresh directory that
// holds a same-named file with secret content, and returns the outside file.
func swapDirForOutsideLink(t *testing.T, dir, name string) string {
	t.Helper()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, name)
	writeTestFile(t, outside, "secret")
	if err := os.Rename(dir, dir+".orig"); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outsideDir, dir)
	return outside
}

// an ancestor directory swapped for a symlink to an outside directory after the
// walk read the entry but before it is statted must not pin the outside file.
func TestCollectModelFiles_AncestorSwappedDuringScanRefused(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "sub", "model.bin"), "original")
	// resolve like the scan does so the hook compares against the walked path
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(resolvedRoot, "sub")

	orig := afterScanEntryHook
	t.Cleanup(func() { afterScanEntryHook = orig })
	swapped := false
	afterScanEntryHook = func(path string) {
		if swapped || path != filepath.Join(sub, "model.bin") {
			return
		}
		swapped = true
		swapDirForOutsideLink(t, sub, "model.bin")
	}

	files, err := collectModelFiles(root)
	if !swapped {
		t.Fatal("hook did not run")
	}
	if err == nil {
		t.Fatalf("expected the swapped ancestor to be refused, collected %+v", files)
	}
	if len(files) != 0 {
		t.Fatalf("nothing may be collected, got %+v", files)
	}
}

// the same swap after a successful scan, before the upload opens the file: the
// outside file must not be uploaded.
func TestUpload_AncestorSwappedAfterScanNotUploaded(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "sub", "model.bin"), "original")

	files, err := collectModelFiles(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("collect: %v %+v", err, files)
	}
	file := files[0]
	swapDirForOutsideLink(t, filepath.Dir(file.AbsolutePath), "model.bin")

	server, hits := newUnreachableUploadServer(t)
	err = completeModelUploadFile(t.Context(), uploadFor(server.URL), file.uploadPath(), file.info, nil)
	if err == nil {
		t.Fatal("expected the file beneath a swapped ancestor to be refused")
	}
	if hits() != 0 {
		t.Fatalf("nothing may be sent, server saw %d request(s)", hits())
	}
}

// openScannedModelFile must refuse an ancestor swapped for a symlink to an
// outside directory even when the outside file has the scanned content (and
// would therefore pass a content check), and must not hand back its descriptor.
func TestOpenScannedModelFile_AncestorSymlinkEscapeRefused(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "sub", "model.bin"), "secret")
	files, err := collectModelFiles(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("collect: %v %+v", err, files)
	}
	file := files[0]
	swapDirForOutsideLink(t, filepath.Dir(file.AbsolutePath), "model.bin")

	f, _, err := openScannedModelFile(file.uploadPath(), file.info)
	if err == nil {
		_ = f.Close()
		t.Fatal("the open escaped the model directory")
	}
}

// replacing the model directory itself after the scan is refused too.
func TestOpenScannedModelFile_RootReplacedAfterScanRefused(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "model")
	writeTestFile(t, filepath.Join(root, "model.bin"), "original")
	files, err := collectModelFiles(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("collect: %v %+v", err, files)
	}
	file := files[0]
	if err := os.Rename(root, root+".orig"); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "model.bin"), "original")

	f, _, err := openScannedModelFile(file.uploadPath(), file.info)
	if err == nil {
		_ = f.Close()
		t.Fatal("expected a replaced model directory to be refused")
	}
}
