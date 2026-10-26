package nvtransfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// setup shrinks every size to a few kib, makes retries instant and keeps
// upload state in a temp dir.
func setup(t *testing.T) (*fakeS3, Options) {
	t.Helper()
	oldSingle, oldPart, oldChunk := singleRequestLimit, minPartSize, chunkSize
	oldPolicy, oldStateDir := retryPolicy, stateDir
	singleRequestLimit, minPartSize, chunkSize = 4<<10, 4<<10, 4<<10
	retryPolicy.baseDelay = time.Millisecond
	dir := t.TempDir()
	stateDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() {
		singleRequestLimit, minPartSize, chunkSize = oldSingle, oldPart, oldChunk
		retryPolicy, stateDir = oldPolicy, oldStateDir
	})
	fake := newFakeS3()
	return fake, Options{Client: fake, Endpoint: "https://s3api-test.runpod.io/", VolumeID: "vol1", Concurrency: 3}
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(int64(n))).Read(b)
	return b
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func stateFiles(t *testing.T) []string {
	t.Helper()
	dir, _ := stateDir()
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestUploadDownloadRoundTripFile(t *testing.T) {
	fake, opts := setup(t)
	ctx := context.Background()
	for _, size := range []int{0, 100, 4 << 10, 10<<10 + 7} {
		data := randomBytes(size)
		src := filepath.Join(t.TempDir(), "model.bin")
		writeFile(t, src, data)

		sum, err := Upload(ctx, opts, src, "models/")
		if err != nil {
			t.Fatalf("size %d: upload: %v", size, err)
		}
		if sum.FilesTransferred != 1 || sum.BytesTransferred != int64(size) {
			t.Fatalf("size %d: summary %+v", size, sum)
		}
		got, ok := fake.get("models/model.bin")
		if !ok || !bytes.Equal(got, data) {
			t.Fatalf("size %d: remote content mismatch", size)
		}

		dst := filepath.Join(t.TempDir(), "out.bin")
		if _, err := Download(ctx, opts, "models/model.bin", dst); err != nil {
			t.Fatalf("size %d: download: %v", size, err)
		}
		local, err := os.ReadFile(dst)
		if err != nil || !bytes.Equal(local, data) {
			t.Fatalf("size %d: local content mismatch", size)
		}
	}
	if left := stateFiles(t); len(left) != 0 {
		t.Fatalf("upload state left behind: %v", left)
	}
}

func TestUploadDirectorySkipsUnchanged(t *testing.T) {
	fake, opts := setup(t)
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "data")
	files := map[string][]byte{
		"a.txt":         []byte("alpha"),
		"sub/b.bin":     randomBytes(9 << 10),
		"sub/deep/c.md": []byte("charlie"),
	}
	for name, data := range files {
		writeFile(t, filepath.Join(src, filepath.FromSlash(name)), data)
	}

	sum, err := Upload(ctx, opts, src, "")
	if err != nil {
		t.Fatal(err)
	}
	if sum.FilesTransferred != 3 || sum.FilesSkipped != 0 {
		t.Fatalf("first run summary %+v", sum)
	}
	for name, data := range files {
		got, ok := fake.get("data/" + name)
		if !ok || !bytes.Equal(got, data) {
			t.Fatalf("data/%s missing or wrong", name)
		}
	}

	// a rerun sends nothing
	sum, err = Upload(ctx, opts, src, "")
	if err != nil {
		t.Fatal(err)
	}
	if sum.FilesTransferred != 0 || sum.FilesSkipped != 3 {
		t.Fatalf("rerun summary %+v", sum)
	}

	// a changed file is sent again
	future := time.Now().Add(time.Hour)
	writeFile(t, filepath.Join(src, "a.txt"), []byte("ALPHA"))
	os.Chtimes(filepath.Join(src, "a.txt"), future, future)
	sum, err = Upload(ctx, opts, src, "")
	if err != nil {
		t.Fatal(err)
	}
	if sum.FilesTransferred != 1 || sum.FilesSkipped != 2 {
		t.Fatalf("changed-file summary %+v", sum)
	}
	if got, _ := fake.get("data/a.txt"); string(got) != "ALPHA" {
		t.Fatalf("changed file not re-uploaded: %q", got)
	}
}

func TestMultipartUploadResumesAfterFailure(t *testing.T) {
	fake, opts := setup(t)
	ctx := context.Background()
	data := randomBytes(10*(4<<10) + 123) // 11 parts
	src := filepath.Join(t.TempDir(), "big.bin")
	writeFile(t, src, data)

	// parts 7+ are refused, so the first run stops partway; one at a time
	// keeps the set of finished parts deterministic
	opts.Concurrency = 1
	fake.onUploadPart = func(part int32) error {
		if part >= 7 {
			return errForbidden
		}
		return nil
	}
	if _, err := Upload(ctx, opts, src, "big.bin"); err == nil {
		t.Fatal("expected the first run to fail")
	}
	if len(stateFiles(t)) != 1 {
		t.Fatal("expected upload state to survive the failure")
	}
	firstRun := fake.count("UploadPart")
	if firstRun < 6 {
		t.Fatalf("expected parts 1-6 uploaded, got %d", firstRun)
	}

	// corrupt part 2 server-side: the rerun must notice the md5 and resend it
	fake.mu.Lock()
	for _, up := range fake.uploads {
		up.parts[2] = bytes.Repeat([]byte{'x'}, len(up.parts[2]))
	}
	fake.mu.Unlock()

	fake.onUploadPart = nil
	if _, err := Upload(ctx, opts, src, "big.bin"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	resent := fake.count("UploadPart") - firstRun
	// parts 7-11 plus the corrupted part 2
	if resent != 6 {
		t.Fatalf("expected 6 parts resent, got %d", resent)
	}
	if fake.count("CreateMultipartUpload") != 1 {
		t.Fatalf("resume started a new upload")
	}
	got, _ := fake.get("big.bin")
	if !bytes.Equal(got, data) {
		t.Fatal("assembled file differs from the source")
	}
	if len(stateFiles(t)) != 0 {
		t.Fatal("state not removed after completion")
	}
}

func TestMultipartUploadRestartsWhenFileChanged(t *testing.T) {
	fake, opts := setup(t)
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "big.bin")
	writeFile(t, src, randomBytes(5*(4<<10)))

	fake.onUploadPart = func(part int32) error {
		if part == 3 {
			return errForbidden
		}
		return nil
	}
	if _, err := Upload(ctx, opts, src, "big.bin"); err == nil {
		t.Fatal("expected failure")
	}
	fake.onUploadPart = nil

	changed := randomBytes(6 * (4 << 10))
	writeFile(t, src, changed)
	if _, err := Upload(ctx, opts, src, "big.bin"); err != nil {
		t.Fatal(err)
	}
	if fake.count("AbortMultipartUpload") != 1 || fake.count("CreateMultipartUpload") != 2 {
		t.Fatalf("expected the stale upload aborted and a new one started, calls %v", fake.calls)
	}
	if got, _ := fake.get("big.bin"); !bytes.Equal(got, changed) {
		t.Fatal("uploaded content is not the changed file")
	}
}

func TestUploadRetriesTransientErrors(t *testing.T) {
	fake, opts := setup(t)
	src := filepath.Join(t.TempDir(), "big.bin")
	data := randomBytes(3 * (4 << 10))
	writeFile(t, src, data)

	var failures atomic.Int32
	fake.onUploadPart = func(part int32) error {
		if part == 2 && failures.Add(1) <= 2 {
			return statusError(524)
		}
		return nil
	}
	if _, err := Upload(context.Background(), opts, src, "big.bin"); err != nil {
		t.Fatal(err)
	}
	if failures.Load() != 3 {
		t.Fatalf("expected two 524s then success, got %d attempts", failures.Load())
	}
	if got, _ := fake.get("big.bin"); !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
}

func TestCompleteTimeoutTreatedAsSuccessWhenObjectExists(t *testing.T) {
	fake, opts := setup(t)
	src := filepath.Join(t.TempDir(), "big.bin")
	data := randomBytes(3 * (4 << 10))
	writeFile(t, src, data)

	// the server assembles the file but the response is lost to a 524; the
	// retry then finds the upload gone
	fake.onComplete = func() error { return statusError(524) }
	if _, err := Upload(context.Background(), opts, src, "big.bin"); err != nil {
		t.Fatalf("expected success via head check, got %v", err)
	}
	if got, _ := fake.get("big.bin"); !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
}

func TestFailedCompleteDoesNotAcceptAnOldObject(t *testing.T) {
	for name, completeErr := range map[string]error{
		"ambiguous 524, old object differs": statusError(524),
		"definite failure":                  apiError("InvalidPart"),
	} {
		t.Run(name, func(t *testing.T) {
			fake, opts := setup(t)
			src := filepath.Join(t.TempDir(), "big.bin")
			data := randomBytes(3 * (4 << 10))
			writeFile(t, src, data)
			// an older object of the same size already sits at the key
			fake.objects["big.bin"] = &fakeObject{data: bytes.Repeat([]byte{'o'}, len(data)), modified: time.Now().Add(-time.Hour)}
			fake.onCompleteBefore = func() error { return completeErr }
			if _, err := Upload(context.Background(), opts, src, "big.bin"); err == nil {
				t.Fatal("a failed complete was reported as success")
			}
			if len(stateFiles(t)) != 1 {
				t.Fatal("upload state must survive so a rerun can complete")
			}
		})
	}
}

func TestUnverifiablePartsAreUploadedAgain(t *testing.T) {
	fake, opts := setup(t)
	fake.opaquePartETags = true
	opts.Concurrency = 1
	src := filepath.Join(t.TempDir(), "big.bin")
	data := randomBytes(5 * (4 << 10))
	writeFile(t, src, data)
	fake.onUploadPart = func(part int32) error {
		if part == 4 {
			return errForbidden
		}
		return nil
	}
	if _, err := Upload(context.Background(), opts, src, "big.bin"); err == nil {
		t.Fatal("expected failure")
	}
	first := fake.count("UploadPart")
	fake.onUploadPart = nil
	if _, err := Upload(context.Background(), opts, src, "big.bin"); err != nil {
		t.Fatal(err)
	}
	if resent := fake.count("UploadPart") - first; resent != 5 {
		t.Fatalf("parts without an md5 etag must be resent: %d of 5", resent)
	}
	if got, _ := fake.get("big.bin"); !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
}

func TestLocalFileChangedDuringUploadAborts(t *testing.T) {
	fake, opts := setup(t)
	opts.Concurrency = 1
	src := filepath.Join(t.TempDir(), "big.bin")
	writeFile(t, src, randomBytes(3*(4<<10)))
	fake.onUploadPart = func(part int32) error {
		if part == 2 {
			future := time.Now().Add(time.Hour)
			os.WriteFile(src, bytes.Repeat([]byte{'n'}, 3*(4<<10)), 0o644)
			os.Chtimes(src, future, future)
		}
		return nil
	}
	_, err := Upload(context.Background(), opts, src, "big.bin")
	if !errors.Is(err, errLocalChanged) {
		t.Fatalf("expected errLocalChanged, got %v", err)
	}
	if fake.count("AbortMultipartUpload") != 1 || fake.count("CompleteMultipartUpload") != 0 {
		t.Fatalf("expected abort and no complete, calls %v", fake.calls)
	}
	if len(stateFiles(t)) != 0 {
		t.Fatal("state of an aborted upload must be removed")
	}
}

func TestUnsavableStateAbortsTheUpload(t *testing.T) {
	fake, opts := setup(t)
	if os.Geteuid() == 0 {
		t.Skip("root writes through directory permissions")
	}
	// readable but not writable: no state is found, and none can be saved
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readOnly, 0o700) })
	stateDir = func() (string, error) { return readOnly, nil }
	src := filepath.Join(t.TempDir(), "big.bin")
	writeFile(t, src, randomBytes(3*(4<<10)))
	if _, err := Upload(context.Background(), opts, src, "big.bin"); err == nil {
		t.Fatal("expected failure")
	}
	if fake.count("AbortMultipartUpload") != 1 {
		t.Fatalf("an upload whose id cannot be saved must be aborted, calls %v", fake.calls)
	}
}

func TestNonRetryableErrorIsNotRetried(t *testing.T) {
	fake, opts := setup(t)
	src := filepath.Join(t.TempDir(), "big.bin")
	writeFile(t, src, randomBytes(2*(4<<10)+1))
	var calls atomic.Int32
	fake.onUploadPart = func(int32) error { calls.Add(1); return errForbidden }
	opts.Concurrency = 1
	if _, err := Upload(context.Background(), opts, src, "big.bin"); err == nil {
		t.Fatal("expected failure")
	}
	if calls.Load() != 1 {
		t.Fatalf("403 was retried: %d calls", calls.Load())
	}
}

func TestDownloadResumesFromSidecar(t *testing.T) {
	fake, opts := setup(t)
	ctx := context.Background()
	data := randomBytes(8*(4<<10) + 99) // 9 chunks
	fake.put("ckpt/model.bin", data)
	dst := filepath.Join(t.TempDir(), "model.bin")

	opts.Concurrency = 1
	fake.onGetObject = func(rng string) error {
		if strings.HasPrefix(rng, "bytes=16384-") { // chunk 4
			return errForbidden
		}
		return nil
	}
	if _, err := Download(ctx, opts, "ckpt/model.bin", dst); err == nil {
		t.Fatal("expected the first run to fail")
	}
	if _, err := os.Stat(dst + sidecarSuffix); err != nil {
		t.Fatal("expected a sidecar after the failure")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("final file must not exist before the download completes")
	}
	firstRun := fake.count("GetObject")

	fake.onGetObject = nil
	if _, err := Download(ctx, opts, "ckpt/model.bin", dst); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if fetched := fake.count("GetObject") - firstRun; fetched != 9-firstRun {
		t.Fatalf("resume refetched finished chunks: %d gets after %d", fetched, firstRun)
	}
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("downloaded content mismatch")
	}
	for _, leftover := range []string{dst + partialSuffix, dst + sidecarSuffix} {
		if _, err := os.Stat(leftover); err == nil {
			t.Fatalf("%s left behind", leftover)
		}
	}

	// a rerun with the file in place is a skip
	sum, err := Download(ctx, opts, "ckpt/model.bin", dst)
	if err != nil || sum.FilesSkipped != 1 {
		t.Fatalf("expected skip, got %+v %v", sum, err)
	}
}

func TestDownloadRefetchesNewerSameSizeRemote(t *testing.T) {
	fake, opts := setup(t)
	ctx := context.Background()
	fake.put("f.txt", []byte("version-1"))
	dst := filepath.Join(t.TempDir(), "f.txt")
	if _, err := Download(ctx, opts, "f.txt", dst); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.objects["f.txt"] = &fakeObject{data: []byte("version-2"), modified: time.Now().Add(time.Minute)}
	fake.mu.Unlock()
	sum, err := Download(ctx, opts, "f.txt", dst)
	if err != nil || sum.FilesTransferred != 1 {
		t.Fatalf("expected a refetch, got %+v %v", sum, err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "version-2" {
		t.Fatalf("stale local copy kept: %q", got)
	}
}

func TestDownloadIgnoresSidecarOfAnotherKey(t *testing.T) {
	fake, opts := setup(t)
	data := randomBytes(4 * (4 << 10))
	fake.put("b.bin", data)
	dir := t.TempDir()
	dst := filepath.Join(dir, "x.bin")
	// a partial download of a different key with the same size and etag
	writeFile(t, dst+partialSuffix, bytes.Repeat([]byte{'z'}, len(data)))
	side, _ := json.Marshal(downloadState{Key: "a.bin", Size: int64(len(data)), ETag: trimETag(etagOf(data)), ChunkSize: chunkSize, Done: []int{0, 1, 2, 3}})
	writeFile(t, dst+sidecarSuffix, side)
	if _, err := Download(context.Background(), opts, "b.bin", dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data) {
		t.Fatal("chunks of another key were reused")
	}
}

func TestDownloadRefusesPartialNameCollision(t *testing.T) {
	fake, opts := setup(t)
	fake.put("d/x", []byte("x"))
	fake.put("d/x.rpdownload", []byte("y"))
	_, err := Download(context.Background(), opts, "d/", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "partial download") {
		t.Fatalf("expected a collision refusal, got %v", err)
	}
}

func TestDownloadRestartsWhenRemoteChanged(t *testing.T) {
	fake, opts := setup(t)
	ctx := context.Background()
	fake.put("f.bin", randomBytes(4*(4<<10)))
	dst := filepath.Join(t.TempDir(), "f.bin")
	opts.Concurrency = 1
	fake.onGetObject = func(rng string) error {
		if strings.HasPrefix(rng, "bytes=8192-") {
			return errForbidden
		}
		return nil
	}
	if _, err := Download(ctx, opts, "f.bin", dst); err == nil {
		t.Fatal("expected failure")
	}
	fake.onGetObject = nil
	replaced := randomBytes(4*(4<<10) + 5)
	fake.put("f.bin", replaced)
	if _, err := Download(ctx, opts, "f.bin", dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, replaced) {
		t.Fatal("stale chunks mixed into the new version")
	}
}

func TestDownloadDirectory(t *testing.T) {
	fake, opts := setup(t)
	ctx := context.Background()
	fake.put("data/a.txt", []byte("alpha"))
	fake.put("data/sub/b.bin", randomBytes(9<<10))
	fake.put("data/sub/", nil) // runpod lists directories
	fake.put(".s3compat_uploads/junk", []byte("x"))
	fake.put("other/c.txt", []byte("not mine"))

	dst := filepath.Join(t.TempDir(), "out")
	// "data" without a slash is detected as a directory
	sum, err := Download(ctx, opts, "data", dst)
	if err != nil {
		t.Fatal(err)
	}
	if sum.FilesTransferred != 2 {
		t.Fatalf("summary %+v", sum)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(got) != "alpha" {
		t.Fatal("a.txt wrong")
	}
	want, _ := fake.get("data/sub/b.bin")
	if got, _ := os.ReadFile(filepath.Join(dst, "sub", "b.bin")); !bytes.Equal(got, want) {
		t.Fatal("sub/b.bin wrong")
	}
	sum, err = Download(ctx, opts, "data/", dst)
	if err != nil || sum.FilesSkipped != 2 || sum.FilesTransferred != 0 {
		t.Fatalf("rerun: %+v %v", sum, err)
	}
}

func TestDownloadRefusesKeysEscapingDestination(t *testing.T) {
	fake, opts := setup(t)
	fake.put("data/../../evil.txt", []byte("x"))
	parent := t.TempDir()
	dst := filepath.Join(parent, "out")
	_, err := Download(context.Background(), opts, "data/", dst)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "..", "evil.txt")); err == nil {
		t.Fatal("file escaped the destination")
	}
}

func TestDownloadMissingPath(t *testing.T) {
	_, opts := setup(t)
	_, err := Download(context.Background(), opts, "nope", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestCleanKey(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"/":           "",
		"a/b":         "a/b",
		"/a//b/":      "a/b/",
		"./a/./b":     "a/b",
		`win\path\f`:  "win/path/f",
		"models/x.pt": "models/x.pt",
	}
	for in, want := range cases {
		got, err := cleanKey(in)
		if err != nil || got != want {
			t.Errorf("cleanKey(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := cleanKey("a/../b"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("expected '..' to be rejected with ErrInvalidPath, got %v", err)
	}
}

func TestPartSizeFor(t *testing.T) {
	cases := []struct {
		size, want int64
	}{
		{1 << 30, 64 << 20},
		{1 << 40, 105 << 20}, // 1 tib needs ~105 mib parts to stay under 10000
		{4 << 40, 420 << 20}, // 4 tib, the largest volume
	}
	for _, c := range cases {
		got, err := partSizeFor(c.size)
		if err != nil || got != c.want {
			t.Errorf("partSizeFor(%d) = %d, %v; want %d", c.size, got, err, c.want)
		}
		if partCount(c.size, got) > maxParts {
			t.Errorf("size %d needs %d parts", c.size, partCount(c.size, got))
		}
	}
	if _, err := partSizeFor(6 << 40); err == nil {
		t.Error("expected an error above the part size limit")
	}
}

func TestRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{statusError(524), true},
		{statusError(503), true},
		{statusError(429), true},
		{statusError(403), false},
		{statusError(404), false},
		{context.DeadlineExceeded, true},
		{transientError{errors.New("eof")}, true},
		{errRemoteChanged, false},
	}
	for _, c := range cases {
		if got := retryable(c.err); got != c.want {
			t.Errorf("retryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
