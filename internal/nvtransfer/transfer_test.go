package nvtransfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// setup shrinks every size to a few kib, makes retries instant and keeps
// upload state in a temp dir.
func setup(t *testing.T) (*fakeS3, Volume) {
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
	return fake, Volume{Client: fake, Endpoint: "https://s3api-test.runpod.io/", ID: "vol1", Concurrency: 3}
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
	fake, vol := setup(t)
	ctx := context.Background()
	for _, size := range []int{0, 100, 4 << 10, 10<<10 + 7} {
		data := randomBytes(size)
		src := filepath.Join(t.TempDir(), "model.bin")
		writeFile(t, src, data)

		sum, err := vol.Upload(ctx, src, "models/")
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
		if _, err := vol.Download(ctx, "models/model.bin", dst); err != nil {
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
	fake, vol := setup(t)
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

	sum, err := vol.Upload(ctx, src, "")
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
	sum, err = vol.Upload(ctx, src, "")
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
	sum, err = vol.Upload(ctx, src, "")
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
	fake, vol := setup(t)
	ctx := context.Background()
	data := randomBytes(10*(4<<10) + 123) // 11 parts
	src := filepath.Join(t.TempDir(), "big.bin")
	writeFile(t, src, data)

	// parts 7+ are refused, so the first run stops partway; one at a time
	// keeps the set of finished parts deterministic
	vol.Concurrency = 1
	fake.onUploadPart = func(part int32) error {
		if part >= 7 {
			return errForbidden
		}
		return nil
	}
	if _, err := vol.Upload(ctx, src, "big.bin"); err == nil {
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
	if _, err := vol.Upload(ctx, src, "big.bin"); err != nil {
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
	fake, vol := setup(t)
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "big.bin")
	writeFile(t, src, randomBytes(5*(4<<10)))

	fake.onUploadPart = func(part int32) error {
		if part == 3 {
			return errForbidden
		}
		return nil
	}
	if _, err := vol.Upload(ctx, src, "big.bin"); err == nil {
		t.Fatal("expected failure")
	}
	fake.onUploadPart = nil

	changed := randomBytes(6 * (4 << 10))
	writeFile(t, src, changed)
	if _, err := vol.Upload(ctx, src, "big.bin"); err != nil {
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
	fake, vol := setup(t)
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
	if _, err := vol.Upload(context.Background(), src, "big.bin"); err != nil {
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
	fake, vol := setup(t)
	src := filepath.Join(t.TempDir(), "big.bin")
	data := randomBytes(3 * (4 << 10))
	writeFile(t, src, data)

	// the server assembles the file but the response is lost to a 524; the
	// retry then finds the upload gone
	fake.onComplete = func() error { return statusError(524) }
	if _, err := vol.Upload(context.Background(), src, "big.bin"); err != nil {
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
			fake, vol := setup(t)
			src := filepath.Join(t.TempDir(), "big.bin")
			data := randomBytes(3 * (4 << 10))
			writeFile(t, src, data)
			// an older object of the same size already sits at the key
			fake.objects["big.bin"] = &fakeObject{data: bytes.Repeat([]byte{'o'}, len(data)), modified: time.Now().Add(-time.Hour)}
			fake.onCompleteBefore = func() error { return completeErr }
			if _, err := vol.Upload(context.Background(), src, "big.bin"); err == nil {
				t.Fatal("a failed complete was reported as success")
			}
			if len(stateFiles(t)) != 1 {
				t.Fatal("upload state must survive so a rerun can complete")
			}
		})
	}
}

func TestAmbiguousCompleteWithoutVerifiableETagFails(t *testing.T) {
	fake, vol := setup(t)
	fake.opaquePartETags = true
	src := filepath.Join(t.TempDir(), "big.bin")
	data := randomBytes(3 * (4 << 10))
	writeFile(t, src, data)
	fake.objects["big.bin"] = &fakeObject{data: bytes.Repeat([]byte{'o'}, len(data)), modified: time.Now().Add(-time.Hour)}
	fake.onCompleteBefore = func() error { return statusError(524) }
	if _, err := vol.Upload(context.Background(), src, "big.bin"); err == nil {
		t.Fatal("a same-size object was accepted without a verifiable etag")
	}
}

func TestSingleRequestUploadDetectsChangeAfterPut(t *testing.T) {
	fake, vol := setup(t)
	src := filepath.Join(t.TempDir(), "small.txt")
	writeFile(t, src, []byte("original"))
	old := time.Now().Add(-time.Hour)
	os.Chtimes(src, old, old)
	var calls atomic.Int32
	vol.Client = putHook{fakeS3: fake, after: func() {
		if calls.Add(1) == 1 {
			// a same-size edit right after the object was written, with an
			// mtime older than the object, so the skip rule alone would keep
			// the stale "original" on the volume
			os.WriteFile(src, []byte("CHANGED!"), 0o644)
			past := time.Now().Add(-30 * time.Minute)
			os.Chtimes(src, past, past)
		}
	}}
	_, err := vol.Upload(context.Background(), src, "small.txt")
	if !errors.Is(err, errLocalChanged) {
		t.Fatalf("expected errLocalChanged, got %v", err)
	}
	if _, ok := fake.get("small.txt"); ok {
		t.Fatal("the object written across the change must be removed")
	}
	// the rerun the error asks for sends the new version
	if _, err := vol.Upload(context.Background(), src, "small.txt"); err != nil {
		t.Fatal(err)
	}
	if got, _ := fake.get("small.txt"); string(got) != "CHANGED!" {
		t.Fatalf("rerun left %q on the volume", got)
	}
}

func TestFailedDeleteAfterLocalChangeAsksForForce(t *testing.T) {
	fake, vol := setup(t)
	src := filepath.Join(t.TempDir(), "small.txt")
	writeFile(t, src, []byte("original"))
	vol.Client = putHook{fakeS3: fake, deleteErr: errForbidden, after: func() {
		future := time.Now().Add(time.Hour)
		os.WriteFile(src, []byte("CHANGED!"), 0o644)
		os.Chtimes(src, future, future)
	}}
	_, err := vol.Upload(context.Background(), src, "small.txt")
	if !errors.As(err, new(*RefusedError)) || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected a refusal naming --force, got %v", err)
	}
}

func TestVanishedLocalFileKeepsTheUploadedObject(t *testing.T) {
	fake, vol := setup(t)
	src := filepath.Join(t.TempDir(), "small.txt")
	writeFile(t, src, []byte("original"))
	vol.Client = putHook{fakeS3: fake, after: func() { os.Remove(src) }}
	if _, err := vol.Upload(context.Background(), src, "small.txt"); err == nil {
		t.Fatal("expected an error for the vanished file")
	}
	if got, ok := fake.get("small.txt"); !ok || string(got) != "original" {
		t.Fatal("a complete upload of a file that then vanished must be kept")
	}
	if fake.count("DeleteObject") != 0 {
		t.Fatal("nothing should be deleted")
	}
}

func TestHead400ThatIsNotAFolderKeepsItsStatus(t *testing.T) {
	fake, vol := setup(t)
	src := filepath.Join(t.TempDir(), "w.bin")
	writeFile(t, src, []byte("w"))
	vol.Client = headHook{fakeS3: fake, err: statusError(http.StatusBadRequest)}
	_, err := vol.Upload(context.Background(), src, "w.bin")
	if errors.As(err, new(*RefusedError)) || HTTPStatus(err) != http.StatusBadRequest {
		t.Fatalf("expected the 400 itself, got %v", err)
	}
}

// headHook fails every HeadObject with err.
type headHook struct {
	*fakeS3
	err error
}

func (h headHook) HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return nil, h.err
}

// stuckPartsHook pages ListParts forever with the same marker.
type stuckPartsHook struct{ *fakeS3 }

func (stuckPartsHook) ListParts(context.Context, *s3.ListPartsInput, ...func(*s3.Options)) (*s3.ListPartsOutput, error) {
	return &s3.ListPartsOutput{
		Parts:                []types.Part{{PartNumber: aws.Int32(1)}},
		IsTruncated:          aws.Bool(true),
		NextPartNumberMarker: aws.String("1"),
	}, nil
}

func TestListPartsStopsOnARepeatedMarker(t *testing.T) {
	fake, vol := setup(t)
	vol.Client = stuckPartsHook{fake}
	if _, err := newRun(vol).listParts(context.Background(), "k", "u"); err == nil || !strings.Contains(err.Error(), "same page marker") {
		t.Fatalf("expected a repeated-marker error, got %v", err)
	}
}

func TestDownloadRefusesASymlinkedPartial(t *testing.T) {
	fake, vol := setup(t)
	fake.put("x", []byte("remote"))
	dir := t.TempDir()
	other := filepath.Join(dir, "other")
	writeFile(t, other, []byte("keep me"))
	if err := os.Symlink("other", filepath.Join(dir, "x"+partialSuffix)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := vol.Download(context.Background(), "x", filepath.Join(dir, "x"))
	if !errors.As(err, new(*RefusedError)) {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if got, _ := os.ReadFile(other); string(got) != "keep me" {
		t.Fatalf("the symlink target was overwritten: %q", got)
	}
}

func TestDownloadRefusesFileThatIsAlsoAFolder(t *testing.T) {
	fake, vol := setup(t)
	fake.put("d/a", []byte("file"))
	fake.put("d/A/b", []byte("nested"))
	_, err := vol.Download(context.Background(), "d/", t.TempDir())
	if !errors.As(err, new(*RefusedError)) || !strings.Contains(err.Error(), "file and a folder") {
		t.Fatalf("expected a refusal, got %v", err)
	}
}

func TestUploadBodyIdleWatchCancelsAndSeekResets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := newIdleSectionReader(io.NewSectionReader(strings.NewReader("abc"), 0, 3), 30*time.Millisecond, cancel)
	defer r.stop()
	time.Sleep(15 * time.Millisecond)
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("seek must reset the idle window")
	}
	if b, _ := io.ReadAll(r); string(b) != "abc" {
		t.Fatalf("read %q", b)
	}
	// eof ends the watch: a slow server reply is not a stalled body
	time.Sleep(50 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("the watch must end at eof")
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	stalled := newIdleSectionReader(io.NewSectionReader(strings.NewReader("abc"), 0, 3), 10*time.Millisecond, cancel2)
	defer stalled.stop()
	time.Sleep(40 * time.Millisecond)
	if ctx2.Err() == nil {
		t.Fatal("a body nobody reads must be canceled")
	}
}

func TestDownloadMissingFolderIsNotFound(t *testing.T) {
	fake, vol := setup(t)
	fake.put("other/x", []byte("x"))
	_, err := vol.Download(context.Background(), "missing/", t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	// an empty volume root is a valid, empty download
	empty, vol2 := setup(t)
	_ = empty
	if sum, err := vol2.Download(context.Background(), "/", t.TempDir()); err != nil || sum.FilesTransferred != 0 {
		t.Fatalf("empty root: %+v %v", sum, err)
	}
}

func TestForceTransfersUnchangedFiles(t *testing.T) {
	fake, vol := setup(t)
	src := filepath.Join(t.TempDir(), "data")
	writeFile(t, filepath.Join(src, "a.txt"), []byte("alpha"))
	if _, err := vol.Upload(context.Background(), src, ""); err != nil {
		t.Fatal(err)
	}
	vol.Force = true
	sum, err := vol.Upload(context.Background(), src, "")
	if err != nil || sum.FilesTransferred != 1 || sum.FilesSkipped != 0 {
		t.Fatalf("force upload: %+v %v", sum, err)
	}
	dst := t.TempDir()
	if _, err := vol.Download(context.Background(), "data/", dst); err != nil {
		t.Fatal(err)
	}
	sum, err = vol.Download(context.Background(), "data/", dst)
	if err != nil || sum.FilesTransferred != 1 {
		t.Fatalf("force download: %+v %v", sum, err)
	}
	_ = fake
}

func TestUploadOntoRemoteFolderIsRefused(t *testing.T) {
	fake, vol := setup(t)
	fake.put("models/a.bin", []byte("a"))
	src := filepath.Join(t.TempDir(), "w.bin")
	writeFile(t, src, []byte("w"))
	_, err := vol.Upload(context.Background(), src, "models")
	if !errors.As(err, new(*RefusedError)) || !strings.Contains(err.Error(), `"models/"`) {
		t.Fatalf("expected a refusal naming models/, got %v", err)
	}
}

func TestFolderUploadSkipsDownloadLeftovers(t *testing.T) {
	fake, vol := setup(t)
	src := filepath.Join(t.TempDir(), "data")
	writeFile(t, filepath.Join(src, "a.bin"), []byte("a"))
	writeFile(t, filepath.Join(src, "b.bin"+partialSuffix), []byte("partial"))
	writeFile(t, filepath.Join(src, "b.bin"+sidecarSuffix), []byte("{}"))
	sum, err := vol.Upload(context.Background(), src, "")
	if err != nil || sum.FilesTransferred != 1 {
		t.Fatalf("summary %+v %v", sum, err)
	}
	if _, ok := fake.get("data/b.bin" + partialSuffix); ok {
		t.Fatal("a partial download was uploaded as data")
	}
}

func TestDownloadIntoMissingFolderWithTrailingSlash(t *testing.T) {
	fake, vol := setup(t)
	fake.put("w.bin", []byte("w"))
	out := filepath.Join(t.TempDir(), "out") + string(filepath.Separator)
	if _, err := vol.Download(context.Background(), "w.bin", out); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(out, "w.bin")); err != nil || string(got) != "w" {
		t.Fatalf("expected out/w.bin, got %q %v", got, err)
	}
}

func TestDownloadStopsWhenRemoteReplacedMidway(t *testing.T) {
	fake, vol := setup(t)
	vol.Concurrency = 1
	fake.put("f.bin", randomBytes(4*(4<<10)))
	fake.onGetObject = func(rng string) error {
		if strings.HasPrefix(rng, "bytes=8192-") {
			fake.put("f.bin", randomBytes(4*(4<<10) + 1)[:4*(4<<10)])
		}
		return nil
	}
	dst := filepath.Join(t.TempDir(), "f.bin")
	if _, err := vol.Download(context.Background(), "f.bin", dst); !errors.Is(err, errRemoteChanged) {
		t.Fatalf("expected errRemoteChanged, got %v", err)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("a mixed file was moved into place")
	}
}

func TestDownloadSidecarPinnedToETag(t *testing.T) {
	fake, vol := setup(t)
	data := randomBytes(4 * (4 << 10))
	fake.put("b.bin", data)
	dst := filepath.Join(t.TempDir(), "b.bin")
	writeFile(t, dst+partialSuffix, bytes.Repeat([]byte{'z'}, len(data)))
	side, _ := json.Marshal(downloadState{Key: "b.bin", Size: int64(len(data)), ETag: "an-older-version", ChunkSize: chunkSize, Done: []int{0, 1, 2, 3}})
	writeFile(t, dst+sidecarSuffix, side)
	if _, err := vol.Download(context.Background(), "b.bin", dst); err != nil {
		t.Fatal(err)
	}
	if fake.count("GetObject") != 4 {
		t.Fatalf("chunks of another version were reused: %d gets", fake.count("GetObject"))
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
}

func TestCompletedAnywayNegatives(t *testing.T) {
	fake, vol := setup(t)
	r := newRun(vol)
	ctx := context.Background()
	// nothing at the key
	if r.completedAnyway(ctx, statusError(524), "k", 3, "x-1") {
		t.Fatal("accepted a missing object")
	}
	fake.put("k", []byte("abc"))
	if r.completedAnyway(ctx, apiError("InvalidPart"), "k", 3, trimETag(etagOf([]byte("abc")))) {
		t.Fatal("accepted a definite failure")
	}
	if r.completedAnyway(ctx, statusError(524), "k", 4, trimETag(etagOf([]byte("abc")))) {
		t.Fatal("accepted a size mismatch")
	}
	if r.completedAnyway(ctx, statusError(524), "k", 3, "") {
		t.Fatal("accepted without a verifiable etag")
	}
	if !r.completedAnyway(ctx, statusError(524), "k", 3, trimETag(etagOf([]byte("abc")))) {
		t.Fatal("rejected a matching object")
	}
}

func TestSkipRulesCompareWholeSeconds(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	writeFile(t, p, []byte("abc"))
	stamp := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// the local file carries sub-second precision, the server's date does not
	os.Chtimes(p, stamp, stamp.Add(700*time.Millisecond))
	info, _ := os.Stat(p)
	remote := remoteObject{size: 3, modified: stamp}
	if !remoteUnchanged(info, remote) || !downloadUnchanged(info, remote) {
		t.Fatal("a sub-second difference must not count as a change")
	}
	if remoteUnchanged(info, remoteObject{size: 3, modified: stamp.Add(-time.Second)}) {
		t.Fatal("an older remote copy must upload")
	}
	if downloadUnchanged(info, remoteObject{size: 3, modified: stamp.Add(time.Second)}) {
		t.Fatal("a newer remote copy must download")
	}
}

func TestIdleReaderCancelsStalledBody(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	r := newIdleReader(pr, 20*time.Millisecond, cancel)
	defer r.stop()
	go func() { <-ctx.Done(); pw.CloseWithError(ctx.Err()) }()
	if _, err := io.ReadAll(r); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the stalled read to be canceled, got %v", err)
	}
}

// putHook runs hooks around a PutObject, and can fail DeleteObject.
type putHook struct {
	*fakeS3
	before    func() error
	after     func()
	deleteErr error
}

func (h putHook) PutObject(ctx context.Context, in *s3.PutObjectInput, vol ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if h.before != nil {
		if err := h.before(); err != nil {
			return nil, err
		}
	}
	out, err := h.fakeS3.PutObject(ctx, in, vol...)
	if err == nil && h.after != nil {
		h.after()
	}
	return out, err
}

func (h putHook) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, vol ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if h.deleteErr != nil {
		return nil, h.deleteErr
	}
	return h.fakeS3.DeleteObject(ctx, in, vol...)
}

func TestRequestsInFlightNeverExceedConcurrency(t *testing.T) {
	fake, vol := setup(t)
	vol.Concurrency = 2
	fake.callDelay = 2 * time.Millisecond
	src := filepath.Join(t.TempDir(), "data")
	for i := 0; i < 4; i++ {
		writeFile(t, filepath.Join(src, fmt.Sprintf("f%d.bin", i)), randomBytes(3*(4<<10)+i))
		writeFile(t, filepath.Join(src, fmt.Sprintf("s%d.txt", i)), []byte("small"))
	}
	if _, err := vol.Upload(context.Background(), src, "data"); err != nil {
		t.Fatal(err)
	}
	if _, err := vol.Download(context.Background(), "data/", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if p := fake.peak.Load(); p > 2 {
		t.Fatalf("%d requests in flight with concurrency 2", p)
	}
}

func TestUnverifiablePartsAreUploadedAgain(t *testing.T) {
	fake, vol := setup(t)
	fake.opaquePartETags = true
	vol.Concurrency = 1
	src := filepath.Join(t.TempDir(), "big.bin")
	data := randomBytes(5 * (4 << 10))
	writeFile(t, src, data)
	fake.onUploadPart = func(part int32) error {
		if part == 4 {
			return errForbidden
		}
		return nil
	}
	if _, err := vol.Upload(context.Background(), src, "big.bin"); err == nil {
		t.Fatal("expected failure")
	}
	first := fake.count("UploadPart")
	fake.onUploadPart = nil
	if _, err := vol.Upload(context.Background(), src, "big.bin"); err != nil {
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
	fake, vol := setup(t)
	vol.Concurrency = 1
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
	_, err := vol.Upload(context.Background(), src, "big.bin")
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
	fake, vol := setup(t)
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
	if _, err := vol.Upload(context.Background(), src, "big.bin"); err == nil {
		t.Fatal("expected failure")
	}
	if fake.count("AbortMultipartUpload") != 1 {
		t.Fatalf("an upload whose id cannot be saved must be aborted, calls %v", fake.calls)
	}
}

func TestNonRetryableErrorIsNotRetried(t *testing.T) {
	fake, vol := setup(t)
	src := filepath.Join(t.TempDir(), "big.bin")
	writeFile(t, src, randomBytes(2*(4<<10)+1))
	var calls atomic.Int32
	fake.onUploadPart = func(int32) error { calls.Add(1); return errForbidden }
	vol.Concurrency = 1
	if _, err := vol.Upload(context.Background(), src, "big.bin"); err == nil {
		t.Fatal("expected failure")
	}
	if calls.Load() != 1 {
		t.Fatalf("403 was retried: %d calls", calls.Load())
	}
}

func TestDownloadResumesFromSidecar(t *testing.T) {
	fake, vol := setup(t)
	ctx := context.Background()
	data := randomBytes(8*(4<<10) + 99) // 9 chunks
	fake.put("ckpt/model.bin", data)
	dst := filepath.Join(t.TempDir(), "model.bin")

	vol.Concurrency = 1
	fake.onGetObject = func(rng string) error {
		if strings.HasPrefix(rng, "bytes=16384-") { // chunk 4
			return errForbidden
		}
		return nil
	}
	if _, err := vol.Download(ctx, "ckpt/model.bin", dst); err == nil {
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
	if _, err := vol.Download(ctx, "ckpt/model.bin", dst); err != nil {
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
	sum, err := vol.Download(ctx, "ckpt/model.bin", dst)
	if err != nil || sum.FilesSkipped != 1 {
		t.Fatalf("expected skip, got %+v %v", sum, err)
	}
}

func TestDownloadRefetchesNewerSameSizeRemote(t *testing.T) {
	fake, vol := setup(t)
	ctx := context.Background()
	fake.put("f.txt", []byte("version-1"))
	dst := filepath.Join(t.TempDir(), "f.txt")
	if _, err := vol.Download(ctx, "f.txt", dst); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.objects["f.txt"] = &fakeObject{data: []byte("version-2"), modified: time.Now().Add(time.Minute)}
	fake.mu.Unlock()
	sum, err := vol.Download(ctx, "f.txt", dst)
	if err != nil || sum.FilesTransferred != 1 {
		t.Fatalf("expected a refetch, got %+v %v", sum, err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "version-2" {
		t.Fatalf("stale local copy kept: %q", got)
	}
}

func TestDownloadIgnoresSidecarOfAnotherKey(t *testing.T) {
	fake, vol := setup(t)
	data := randomBytes(4 * (4 << 10))
	fake.put("b.bin", data)
	dir := t.TempDir()
	dst := filepath.Join(dir, "x.bin")
	// a partial download of a different key with the same size and etag
	writeFile(t, dst+partialSuffix, bytes.Repeat([]byte{'z'}, len(data)))
	side, _ := json.Marshal(downloadState{Key: "a.bin", Size: int64(len(data)), ETag: trimETag(etagOf(data)), ChunkSize: chunkSize, Done: []int{0, 1, 2, 3}})
	writeFile(t, dst+sidecarSuffix, side)
	if _, err := vol.Download(context.Background(), "b.bin", dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data) {
		t.Fatal("chunks of another key were reused")
	}
}

func TestDownloadRefusesPartialNameCollision(t *testing.T) {
	for name, other := range map[string]string{
		"file":             "d/x.rpdownload",
		"sidecar":          "d/x.rpdownload.json",
		"sidecar temp":     "d/x.rpdownload.json.tmp",
		"directory":        "d/x.rpdownload/y",
		"case-insensitive": "d/X.RPDOWNLOAD",
	} {
		t.Run(name, func(t *testing.T) {
			fake, vol := setup(t)
			fake.put("d/x", []byte("x"))
			fake.put(other, []byte("y"))
			_, err := vol.Download(context.Background(), "d/", t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "partial download") {
				t.Fatalf("expected a collision refusal, got %v", err)
			}
		})
	}
}

func TestDownloadRefusesTargetsThatShareALocalName(t *testing.T) {
	for name, pair := range map[string][2]string{
		"case":          {"d/Model.bin", "d/model.bin"},
		"unicode fold":  {"d/s", "d/\u017f"},
		"normalization": {"d/\u00e9", "d/e\u0301"},
	} {
		t.Run(name, func(t *testing.T) {
			fake, vol := setup(t)
			fake.put(pair[0], []byte("a"))
			fake.put(pair[1], []byte("b"))
			_, err := vol.Download(context.Background(), "d/", t.TempDir())
			if !errors.As(err, new(*RefusedError)) || !strings.Contains(err.Error(), "one file") {
				t.Fatalf("expected a refusal, got %v", err)
			}
		})
	}
}

func TestDownloadRestartsWhenRemoteChanged(t *testing.T) {
	fake, vol := setup(t)
	ctx := context.Background()
	fake.put("f.bin", randomBytes(4*(4<<10)))
	dst := filepath.Join(t.TempDir(), "f.bin")
	vol.Concurrency = 1
	fake.onGetObject = func(rng string) error {
		if strings.HasPrefix(rng, "bytes=8192-") {
			return errForbidden
		}
		return nil
	}
	if _, err := vol.Download(ctx, "f.bin", dst); err == nil {
		t.Fatal("expected failure")
	}
	fake.onGetObject = nil
	replaced := randomBytes(4*(4<<10) + 5)
	fake.put("f.bin", replaced)
	if _, err := vol.Download(ctx, "f.bin", dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, replaced) {
		t.Fatal("stale chunks mixed into the new version")
	}
}

func TestDownloadDirectory(t *testing.T) {
	fake, vol := setup(t)
	ctx := context.Background()
	fake.put("data/a.txt", []byte("alpha"))
	fake.put("data/sub/b.bin", randomBytes(9<<10))
	fake.put("data/sub/", nil) // runpod lists directories
	fake.put(".s3compat_uploads/junk", []byte("x"))
	fake.put("other/c.txt", []byte("not mine"))

	dst := filepath.Join(t.TempDir(), "out")
	// "data" without a slash is detected as a directory
	sum, err := vol.Download(ctx, "data", dst)
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
	sum, err = vol.Download(ctx, "data/", dst)
	if err != nil || sum.FilesSkipped != 2 || sum.FilesTransferred != 0 {
		t.Fatalf("rerun: %+v %v", sum, err)
	}
}

func TestDownloadRefusesKeysEscapingDestination(t *testing.T) {
	fake, vol := setup(t)
	fake.put("data/../../evil.txt", []byte("x"))
	parent := t.TempDir()
	dst := filepath.Join(parent, "out")
	_, err := vol.Download(context.Background(), "data/", dst)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "..", "evil.txt")); err == nil {
		t.Fatal("file escaped the destination")
	}
}

func TestDownloadMissingPath(t *testing.T) {
	_, vol := setup(t)
	_, err := vol.Download(context.Background(), "nope", t.TempDir())
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
		{&smithy.OperationError{Err: &smithyhttp.RequestSendError{Err: errors.New("connection refused")}}, true},
		{&smithy.OperationError{Err: errors.New("invalid parameter")}, false},
		// a lookup that fails mid-transfer is a network drop (a wrong data
		// center is caught before the transfer starts)
		{&smithy.OperationError{Err: &net.DNSError{Name: "s3api-eu-ro-1.runpod.io", IsNotFound: true}}, true},
		{&fs.PathError{Op: "write", Path: "f", Err: syscall.ENOSPC}, false},
	}
	for _, c := range cases {
		if got := retryable(c.err); got != c.want {
			t.Errorf("retryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestDiscardedChunksDropTheSidecarFirst(t *testing.T) {
	_, vol := setup(t)
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f := remoteFile{key: "f.bin", name: "f.bin", obj: remoteObject{size: 3 * chunkSize, etag: "e"}}
	// a sidecar that vouches for every chunk beside a partial of the wrong size
	st := downloadState{Key: f.key, Size: f.obj.size, ETag: "e", ChunkSize: chunkSize, Done: []int{0, 1, 2}}
	if err := writeJSONIn(root, f.name+sidecarSuffix, st, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, f.name+partialSuffix), []byte("short"))

	// stop before any chunk lands, so no new sidecar is written
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &chunkedDownload{run: newRun(vol), root: root, f: f, partial: f.name + partialSuffix, sidecar: f.name + sidecarSuffix}
	_ = d.fetch(ctx)
	if _, err := os.Stat(filepath.Join(dir, f.name+sidecarSuffix)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the stale sidecar survived beside a resized partial: %v", err)
	}
}
