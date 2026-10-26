package nvtransfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	partialSuffix = ".rpdownload"
	sidecarSuffix = ".rpdownload.json"
)

// ErrNotFound is a remote path with nothing at it.
var ErrNotFound = errors.New("not found")

// errRemoteChanged ends a download whose object was replaced mid-transfer.
// It is not retried: the finished chunks belong to the old version.
var errRemoteChanged = errors.New("remote file changed during download; run the command again to restart it")

// transientError marks a failure worth retrying that the sdk does not type,
// such as a connection dropped while reading a response body.
type transientError struct{ error }

func (e transientError) Unwrap() error { return e.error }

type remoteFile struct {
	key  string
	name string // native path relative to the local root
	obj  remoteObject
}

// Download copies a volume file or directory to the local machine.
//
// remotePath is a directory when it ends in "/" or names a prefix with
// objects under it. localPath defaults to the remote base name; a file
// downloaded into an existing local directory keeps its base name. Local files
// with the remote size that are not older than the remote copy are skipped
// (downloads stamp the remote time on the file). Large files are
// fetched in chunks into "<name>.rpdownload" with progress kept in
// "<name>.rpdownload.json", so a rerun continues where it stopped.
func Download(ctx context.Context, opts Options, remotePath, localPath string) (*Summary, error) {
	r := newRun(opts)
	key, err := cleanKey(remotePath)
	if err != nil {
		return nil, err
	}

	var listed map[string]remoteObject
	isDir := key == "" || strings.HasSuffix(key, "/")
	if !isDir {
		obj, err := r.headObject(ctx, key)
		// runpod answers a head on a directory with 400, not 404
		if err != nil && HTTPStatus(err) != http.StatusBadRequest {
			return nil, fmt.Errorf("check %q: %w", key, err)
		}
		if obj != nil {
			return r.downloadOne(ctx, key, *obj, localPath)
		}
		listed, err = r.listPrefix(ctx, key+"/")
		if err != nil {
			return nil, err
		}
		key += "/"
	} else if listed, err = r.listPrefix(ctx, key); err != nil {
		return nil, err
	}
	// a typo must not look like an empty folder; the whole volume may be empty
	if len(listed) == 0 && key != "" {
		return nil, fmt.Errorf("%q on volume %s: %w", strings.TrimSuffix(key, "/"), r.VolumeID, ErrNotFound)
	}

	if localPath == "" {
		localPath = r.VolumeID
		if key != "" {
			localPath = path.Base(strings.TrimSuffix(key, "/"))
		}
	}
	if err := os.MkdirAll(localPath, 0o755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(localPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	var files []remoteFile
	for k, obj := range listed {
		rel := filepath.FromSlash(strings.TrimPrefix(k, key))
		if !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("remote file %q would be written outside %s: %w", k, localPath, ErrRefused)
		}
		files = append(files, remoteFile{key: k, name: rel, obj: obj})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].key < files[j].key })
	if err := checkLocalNames(files); err != nil {
		return nil, err
	}
	return r.downloadAll(ctx, root, files)
}

// checkLocalNames refuses a listing whose files would share a path on disk:
// two targets that are one name on a case-insensitive or normalizing
// filesystem ("Model.bin" and "model.bin"), or a file whose partial or
// sidecar name is also a target or a directory a target lives in ("x" beside
// "x.rpdownload" or "x.rpdownload/y"). Names are compared under foldKey; that
// can refuse a listing a case-sensitive filesystem would take, which is the
// safe direction.
func checkLocalNames(files []remoteFile) error {
	targets := map[string]string{}
	dirs := map[string]string{}
	for _, f := range files {
		k := foldKey(f.name)
		if other, ok := targets[k]; ok {
			return fmt.Errorf("cannot download both %q and %q: they are the same file on a case-insensitive filesystem: %w", other, f.name, ErrRefused)
		}
		targets[k] = f.name
		for p := filepath.Dir(f.name); p != "." && p != string(filepath.Separator); p = filepath.Dir(p) {
			dirs[foldKey(p)] = f.name
		}
	}
	for _, f := range files {
		for _, suffix := range []string{partialSuffix, sidecarSuffix} {
			k := foldKey(f.name + suffix)
			other, ok := targets[k]
			if !ok {
				other, ok = dirs[k]
			}
			if ok {
				return fmt.Errorf("cannot download both %q and %q: %q is used for the first's partial download: %w", f.name, other, f.name+suffix, ErrRefused)
			}
		}
	}
	return nil
}

// folder is unicode case folding, which strings.ToLower is not (it leaves ſ
// and ς alone while case-insensitive filesystems read them as s and σ).
var folder = cases.Fold()

// foldKey is the key under which two spellings a filesystem may treat as one
// name collide: nfc first, since folding does not normalize and apfs compares
// names normalization-insensitively. same rule as cmd/transfer's foldKey.
func foldKey(name string) string {
	return folder.String(norm.NFC.String(name))
}

func (r *run) downloadOne(ctx context.Context, key string, obj remoteObject, localPath string) (*Summary, error) {
	base := path.Base(key)
	if localPath == "" {
		localPath = base
	}
	// a trailing slash names a folder to download into, as on upload
	if st, err := os.Stat(localPath); (err == nil && st.IsDir()) || strings.HasSuffix(localPath, "/") || strings.HasSuffix(localPath, string(filepath.Separator)) {
		localPath = filepath.Join(localPath, base)
	}
	abs, err := filepath.Abs(localPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Dir(abs))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return r.downloadAll(ctx, root, []remoteFile{{key: key, name: filepath.Base(abs), obj: obj}})
}

func (r *run) downloadAll(ctx context.Context, root *os.Root, files []remoteFile) (*Summary, error) {
	var todo []remoteFile
	var total int64
	for _, f := range files {
		if st, err := root.Stat(f.name); err == nil && !r.Force && st.Mode().IsRegular() && downloadUnchanged(st, f.obj) {
			r.skipped()
			continue
		}
		todo = append(todo, f)
		total += f.obj.size
	}
	r.Reporter.Start(len(todo), total)
	err := forEach(ctx, r.Concurrency, todo, func(ctx context.Context, f remoteFile) error {
		if err := r.downloadFile(ctx, root, f); err != nil {
			return fmt.Errorf("download %s: %w", f.key, err)
		}
		r.transferred(f.key)
		return nil
	})
	return &r.summary, err
}

func (r *run) downloadFile(ctx context.Context, root *os.Root, f remoteFile) error {
	if dir := filepath.Dir(f.name); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	partial := f.name + partialSuffix
	sidecar := f.name + sidecarSuffix
	size := f.obj.size

	if size <= chunkSize {
		file, err := root.OpenFile(partial, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		err = r.getRange(ctx, file, f.key, 0, size, size, f.obj.etag)
		if err == nil {
			err = file.Sync()
		}
		if cerr := file.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		return finish(root, partial, f, "")
	}

	chunks := int((size + chunkSize - 1) / chunkSize)
	done := map[int]bool{}
	var st downloadState
	if data, err := root.ReadFile(sidecar); err == nil && json.Unmarshal(data, &st) == nil &&
		st.Key == f.key && st.Size == size && st.ETag == f.obj.etag && st.ChunkSize == chunkSize {
		if info, err := root.Stat(partial); err == nil && info.Size() == size {
			for _, c := range st.Done {
				done[c] = true
			}
		}
	}

	file, err := root.OpenFile(partial, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if len(done) == 0 {
		if err := file.Truncate(0); err != nil {
			return err
		}
	}
	if err := file.Truncate(size); err != nil {
		return err
	}

	st = downloadState{Key: f.key, Size: size, ETag: f.obj.etag, ChunkSize: chunkSize}
	var missing []int
	for c := 0; c < chunks; c++ {
		if done[c] {
			st.Done = append(st.Done, c)
			r.Reporter.Reused(chunkLen(c, size))
		} else {
			missing = append(missing, c)
		}
	}

	var mu sync.Mutex
	err = forEach(ctx, r.Concurrency, missing, func(ctx context.Context, c int) error {
		off := int64(c) * chunkSize
		length := chunkLen(c, size)
		if err := r.getRange(ctx, file, f.key, off, length, size, f.obj.etag); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		// data reaches disk before the sidecar claims it
		if err := file.Sync(); err != nil {
			return err
		}
		st.Done = append(st.Done, c)
		slices.Sort(st.Done)
		return writeJSONIn(root, sidecar, st, 0o644)
	})
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return finish(root, partial, f, sidecar)
}

func chunkLen(c int, size int64) int64 {
	off := int64(c) * chunkSize
	return min(chunkSize, size-off)
}

// finish moves the completed partial file into place and stamps it with the
// remote modification time, so uploading it back is recognized as unchanged.
func finish(root *os.Root, partial string, f remoteFile, sidecar string) error {
	info, err := root.Stat(partial)
	if err != nil {
		return err
	}
	if info.Size() != f.obj.size {
		return fmt.Errorf("downloaded %d bytes, expected %d", info.Size(), f.obj.size)
	}
	if err := root.Rename(partial, f.name); err != nil {
		return err
	}
	if !f.obj.modified.IsZero() {
		_ = root.Chtimes(f.name, time.Now(), f.obj.modified)
	}
	if sidecar != "" {
		_ = root.Remove(sidecar)
	}
	return nil
}

// getRange writes bytes [off, off+length) of the object into file at off.
func (r *run) getRange(ctx context.Context, file *os.File, key string, off, length, size int64, etag string) error {
	return r.call(ctx, transferTimeout(length), func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		in := &s3.GetObjectInput{Bucket: aws.String(r.VolumeID), Key: aws.String(key)}
		if off != 0 || length != size {
			in.Range = aws.String(fmt.Sprintf("bytes=%d-%d", off, off+length-1))
		}
		out, err := r.Client.GetObject(ctx, in)
		if err != nil {
			return err
		}
		defer out.Body.Close()
		if got := trimETag(aws.ToString(out.ETag)); etag != "" && got != "" && got != etag {
			return errRemoteChanged
		}
		dst := &offsetWriter{w: io.NewOffsetWriter(file, off)}
		body := newIdleReader(out.Body, idleTimeout, cancel)
		defer body.stop()
		n, err := io.Copy(dst, io.LimitReader(body, length))
		switch {
		case dst.err != nil:
			// a full or failing disk does not get better on retry
			return fmt.Errorf("write %s: %w", file.Name(), dst.err)
		case err != nil:
			return transientError{fmt.Errorf("read response: %w", err)}
		case n != length:
			return transientError{fmt.Errorf("short read: got %d of %d bytes", n, length)}
		}
		r.sentBytes(n)
		return nil
	})
}

// offsetWriter keeps the writer's error apart from the reader's, so a local
// write failure is not mistaken for a dropped connection.
type offsetWriter struct {
	w   io.Writer
	err error
}

func (o *offsetWriter) Write(p []byte) (int, error) {
	n, err := o.w.Write(p)
	if err != nil {
		o.err = err
	}
	return n, err
}

// idleReader cancels a response whose body stops delivering bytes for the
// idle window, so a stalled connection is retried in minutes rather than at
// the end of the attempt's deadline.
type idleReader struct {
	r     io.Reader
	idle  time.Duration
	timer *time.Timer
}

func newIdleReader(r io.Reader, idle time.Duration, cancel context.CancelFunc) *idleReader {
	return &idleReader{r: r, idle: idle, timer: time.AfterFunc(idle, cancel)}
}

func (i *idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if n > 0 {
		i.timer.Reset(i.idle)
	}
	return n, err
}

func (i *idleReader) stop() { i.timer.Stop() }
