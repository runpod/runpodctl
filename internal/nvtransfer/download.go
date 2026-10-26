package nvtransfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	"github.com/runpod/runpodctl/internal/namefold"
)

const (
	partialSuffix = ".rpdownload"
	sidecarSuffix = ".rpdownload.json"
)

// ErrNotFound is a remote path with nothing at it.
var ErrNotFound = errors.New("not found")

// errRemoteChanged ends a download whose object was replaced mid-transfer.
// It is not retried: the finished chunks belong to the old version.
var errRemoteChanged = errors.New("remote file changed during download")

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
func (v Volume) Download(ctx context.Context, remotePath, localPath string) (*Summary, error) {
	r := newRun(v)
	key, err := cleanKey(remotePath)
	if err != nil {
		return nil, err
	}
	if key != "" && !strings.HasSuffix(key, "/") {
		obj, err := r.headObject(ctx, key)
		// runpod answers a head on a directory with 400, not 404
		if err != nil && HTTPStatus(err) != http.StatusBadRequest {
			return nil, fmt.Errorf("check %q: %w", key, err)
		}
		if obj != nil {
			return r.downloadOne(ctx, key, *obj, localPath)
		}
		key += "/"
	}
	return r.downloadFolder(ctx, key, localPath)
}

// downloadFolder downloads every object under prefix into localPath, which
// defaults to the folder's name (the volume id for the whole volume).
func (r *run) downloadFolder(ctx context.Context, prefix, localPath string) (*Summary, error) {
	listed, err := r.listPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}
	// a typo must not look like an empty folder; the whole volume may be empty
	if len(listed) == 0 && prefix != "" {
		return nil, fmt.Errorf("%q on volume %s: %w", strings.TrimSuffix(prefix, "/"), r.ID, ErrNotFound)
	}
	if localPath == "" {
		localPath = r.ID
		if prefix != "" {
			localPath = path.Base(strings.TrimSuffix(prefix, "/"))
		}
	}
	files, err := localTargets(listed, prefix, localPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(localPath, 0o755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(localPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return r.downloadAll(ctx, root, files)
}

// localTargets maps listed objects to paths under the download folder,
// sorted by key, refusing any that would land outside it or collide.
func localTargets(listed map[string]remoteObject, prefix, localPath string) ([]remoteFile, error) {
	var files []remoteFile
	for k, obj := range listed {
		rel := filepath.FromSlash(strings.TrimPrefix(k, prefix))
		if !filepath.IsLocal(rel) {
			return nil, refusef("remote file %q would be written outside %s", k, localPath)
		}
		files = append(files, remoteFile{key: k, name: rel, obj: obj})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].key < files[j].key })
	return files, checkLocalNames(files)
}

// checkLocalNames refuses a listing whose files would share a path on disk:
// two targets that are one name on a case-insensitive or normalizing
// filesystem ("Model.bin" and "model.bin"), a file whose name is also a
// folder another file lives in ("a" beside "A/b"), or a file whose partial or
// sidecar name is taken ("x" beside "x.rpdownload" or "x.rpdownload/y").
// Names are compared under namefold.Key; that can refuse a listing a
// case-sensitive filesystem would take, which is the safe direction.
func checkLocalNames(files []remoteFile) error {
	targets := map[string]string{}
	dirs := map[string]string{}
	for _, f := range files {
		k := namefold.Key(f.name)
		if other, ok := targets[k]; ok {
			return refusef("cannot download both %q and %q: they would be one file on a case-insensitive or normalizing filesystem", other, f.name)
		}
		targets[k] = f.name
		for p := filepath.Dir(f.name); p != "." && p != string(filepath.Separator); p = filepath.Dir(p) {
			dirs[namefold.Key(p)] = f.name
		}
	}
	for k, name := range targets {
		if other, ok := dirs[k]; ok {
			return refusef("cannot download both %q and %q: the first would have to be a file and a folder", name, other)
		}
	}
	return checkWorkNames(files, targets, dirs)
}

// workSuffixes name the files a download writes beside its target.
var workSuffixes = []string{partialSuffix, sidecarSuffix, sidecarSuffix + ".tmp"}

// checkWorkNames refuses a file whose partial, sidecar, or sidecar temp name
// is another target or a folder one lives in.
func checkWorkNames(files []remoteFile, targets, dirs map[string]string) error {
	for _, f := range files {
		for _, suffix := range workSuffixes {
			k := namefold.Key(f.name + suffix)
			other, ok := targets[k]
			if !ok {
				other, ok = dirs[k]
			}
			if ok {
				return refusef("cannot download both %q and %q: %q is used for the first's partial download", f.name, other, f.name+suffix)
			}
		}
	}
	return nil
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
	if err := refuseNonRegular(root, partial); err != nil {
		return err
	}
	if f.obj.size <= chunkSize {
		return r.downloadWhole(ctx, root, f, partial)
	}
	d := &chunkedDownload{run: r, root: root, f: f, partial: partial, sidecar: f.name + sidecarSuffix}
	return d.fetch(ctx)
}

// refuseNonRegular refuses a partial download path that exists as anything
// but a regular file: a symlink there would point the write at another file.
func refuseNonRegular(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return refusef("%q exists and is not a regular file; remove it and rerun", name)
	}
	return nil
}

// downloadWhole fetches a file that fits in one chunk.
func (r *run) downloadWhole(ctx context.Context, root *os.Root, f remoteFile, partial string) error {
	file, err := root.OpenFile(partial, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	err = r.getRange(ctx, file, f.key, 0, f.obj.size, f.obj.size, f.obj.etag)
	if err == nil {
		err = file.Sync()
	}
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return finishDownload(root, partial, f, "")
}

// chunkedDownload fetches a large file in chunks into its partial file,
// recording finished chunks in the sidecar so a rerun fetches only the rest.
type chunkedDownload struct {
	*run
	root    *os.Root
	f       remoteFile
	partial string
	sidecar string
	file    *os.File
	state   downloadState
	mu      sync.Mutex // guards state and the sidecar
}

func (d *chunkedDownload) fetch(ctx context.Context) error {
	done := d.completedChunks()
	if err := d.openPartial(len(done) > 0); err != nil {
		return err
	}
	defer d.file.Close()

	d.state = downloadState{Key: d.f.key, Size: d.f.obj.size, ETag: d.f.obj.etag, ChunkSize: chunkSize}
	var missing []int
	for c := range int((d.f.obj.size + chunkSize - 1) / chunkSize) {
		if done[c] {
			d.state.Done = append(d.state.Done, c)
			d.Reporter.Reused(chunkLen(c, d.f.obj.size))
			continue
		}
		missing = append(missing, c)
	}
	if err := forEach(ctx, d.Concurrency, missing, d.fetchChunk); err != nil {
		return err
	}
	if err := d.file.Close(); err != nil {
		return err
	}
	return finishDownload(d.root, d.partial, d.f, d.sidecar)
}

// completedChunks reads the chunks an earlier run finished, if the sidecar
// describes this version of the object and the partial file is intact.
func (d *chunkedDownload) completedChunks() map[int]bool {
	var st downloadState
	data, err := d.root.ReadFile(d.sidecar)
	if err != nil || json.Unmarshal(data, &st) != nil {
		return nil
	}
	if st.Key != d.f.key || st.Size != d.f.obj.size || st.ETag != d.f.obj.etag || st.ChunkSize != chunkSize {
		return nil
	}
	if info, err := d.root.Stat(d.partial); err != nil || info.Size() != d.f.obj.size {
		return nil
	}
	done := map[int]bool{}
	for _, c := range st.Done {
		done[c] = true
	}
	return done
}

// openPartial opens the partial file at the object's size, emptying it
// first unless finished chunks are kept.
func (d *chunkedDownload) openPartial(keep bool) error {
	file, err := d.root.OpenFile(d.partial, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	d.file = file
	if !keep {
		if err := file.Truncate(0); err != nil {
			return err
		}
	}
	return file.Truncate(d.f.obj.size)
}

func (d *chunkedDownload) fetchChunk(ctx context.Context, c int) error {
	off := int64(c) * chunkSize
	if err := d.getRange(ctx, d.file, d.f.key, off, chunkLen(c, d.f.obj.size), d.f.obj.size, d.f.obj.etag); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	// data reaches disk before the sidecar claims it
	if err := d.file.Sync(); err != nil {
		return err
	}
	d.state.Done = append(d.state.Done, c)
	slices.Sort(d.state.Done)
	return writeJSONIn(d.root, d.sidecar, d.state, 0o644)
}

func chunkLen(c int, size int64) int64 {
	off := int64(c) * chunkSize
	return min(chunkSize, size-off)
}

// finishDownload moves the completed partial file into place and stamps it with the
// remote modification time, so uploading it back is recognized as unchanged.
func finishDownload(root *os.Root, partial string, f remoteFile, sidecar string) error {
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
	return r.call(ctx, requestTimeout, func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		in := &s3.GetObjectInput{Bucket: aws.String(r.ID), Key: aws.String(key)}
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
		r.addBytes(n)
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

// idleReader cancels a request whose body stops moving for the idle window,
// so a stalled connection is retried in minutes rather than at the end of the
// attempt's deadline. On a download it wraps the response body; on an upload
// the request body, which the transport reads only as fast as the connection
// sends, and the watch ends at eof so a slow server reply is left to the
// transport's response header timeout.
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
	if err == io.EOF {
		i.timer.Stop()
	}
	return n, err
}

func (i *idleReader) stop() { i.timer.Stop() }

// idleSectionReader is an idleReader over a seekable section, as the sdk
// needs to sign a request body and rewind it.
type idleSectionReader struct {
	*idleReader
	s *io.SectionReader
}

func newIdleSectionReader(s *io.SectionReader, idle time.Duration, cancel context.CancelFunc) *idleSectionReader {
	return &idleSectionReader{idleReader: newIdleReader(s, idle, cancel), s: s}
}

func (i *idleSectionReader) Seek(offset int64, whence int) (int64, error) {
	i.timer.Reset(i.idle)
	return i.s.Seek(offset, whence)
}
