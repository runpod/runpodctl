package nvtransfer

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type localFile struct {
	path string // absolute
	key  string
	info os.FileInfo
}

// Upload copies a local file or directory to the volume.
//
// remotePath defaults to the local base name. A file uploaded to a remote path
// ending in "/" keeps its base name under that directory. A directory's
// contents land under remotePath. Files whose remote copy has the same size
// and is not older than the local file are skipped, so rerunning an
// interrupted directory upload only sends what is missing.
func (v Volume) Upload(ctx context.Context, localPath, remotePath string) (*Summary, error) {
	r := newRun(v)
	abs, err := filepath.Abs(localPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return nil, refusef("%s is not a regular file or folder", localPath)
	}
	if remotePath == "" {
		remotePath = filepath.Base(abs)
	}
	key, err := cleanKey(remotePath)
	if err != nil {
		return nil, err
	}

	plan := r.planFile
	if info.IsDir() {
		plan = r.planDir
	}
	todo, err := plan(ctx, localFile{path: abs, key: key, info: info})
	if err != nil {
		return nil, err
	}

	var total int64
	for _, f := range todo {
		total += f.info.Size()
	}
	r.Reporter.Start(len(todo), total)
	err = forEach(ctx, r.Concurrency, todo, func(ctx context.Context, f localFile) error {
		if err := r.uploadFile(ctx, f); err != nil {
			return fmt.Errorf("upload %s: %w", f.key, err)
		}
		r.transferred(f.key)
		return nil
	})
	return &r.summary, err
}

// planDir lists the files under dir.path to upload under dir.key, counting
// unchanged files and partial-download leftovers as skipped.
func (r *run) planDir(ctx context.Context, dir localFile) ([]localFile, error) {
	files, leftovers, err := walkLocal(dir.path, dir.key)
	if err != nil {
		return nil, err
	}
	for range leftovers {
		r.skipped()
	}
	remote, err := r.listPrefix(ctx, dirPrefix(dir.key))
	if err != nil {
		return nil, err
	}
	var todo []localFile
	for _, f := range files {
		if obj, ok := remote[f.key]; ok && !r.Force && remoteUnchanged(f.info, obj) {
			r.skipped()
			continue
		}
		todo = append(todo, f)
	}
	return todo, nil
}

// planFile returns the single file to upload, or none when the remote copy
// is unchanged. A key ending in "/" (or empty) names a folder to upload into.
func (r *run) planFile(ctx context.Context, f localFile) ([]localFile, error) {
	if f.key == "" || strings.HasSuffix(f.key, "/") {
		f.key += filepath.Base(f.path)
	}
	obj, err := r.headObject(ctx, f.key)
	// runpod answers a head on a directory with 400, but a head carries
	// no body, so a 400 for any other reason looks the same: confirm
	if HTTPStatus(err) == http.StatusBadRequest && r.remoteFolderExists(ctx, f.key) {
		return nil, refusef("%q is a folder on the volume; use %q to upload into it", f.key, f.key+"/")
	}
	if err != nil {
		return nil, fmt.Errorf("check %q: %w", f.key, err)
	}
	if obj != nil && !r.Force && remoteUnchanged(f.info, *obj) {
		r.skipped()
		return nil, nil
	}
	return []localFile{f}, nil
}

// dirPrefix is key as a folder prefix: "" stays the volume root.
func dirPrefix(key string) string {
	if key == "" || strings.HasSuffix(key, "/") {
		return key
	}
	return key + "/"
}

// walkLocal lists the regular files under root (following symlinked files)
// with their keys under prefix, and counts the partial-download leftovers it
// leaves out.
func walkLocal(root, prefix string) ([]localFile, int, error) {
	var files []localFile
	leftovers := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if isDownloadLeftover(p) {
			leftovers++
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, localFile{path: p, key: joinKey(prefix, filepath.ToSlash(rel)), info: info})
		return nil
	})
	return files, leftovers, err
}

func (r *run) uploadFile(ctx context.Context, f localFile) error {
	file, err := os.Open(f.path)
	if err != nil {
		return err
	}
	defer file.Close()
	if f.info.Size() > singleRequestLimit {
		return r.multipartUpload(ctx, file, f)
	}
	if err := r.putObject(ctx, file, f); err != nil {
		return err
	}
	if err := r.keepIfUnchanged(ctx, f); err != nil {
		return err
	}
	r.addBytes(f.info.Size())
	return nil
}

// putObject sends f in one request.
func (r *run) putObject(ctx context.Context, file *os.File, f localFile) error {
	size := f.info.Size()
	return r.call(ctx, requestTimeout, func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		body := newIdleSectionReader(io.NewSectionReader(file, 0, size), idleTimeout, cancel)
		defer body.stop()
		_, err := r.Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(r.ID),
			Key:           aws.String(f.key),
			Body:          body,
			ContentLength: aws.Int64(size),
		})
		return err
	})
}

// keepIfUnchanged checks that the local file did not change while putObject
// read it. If it did, the object may hold either version and is newer than
// the local edit, so a rerun would skip it: remove it so the rerun sends it.
func (r *run) keepIfUnchanged(ctx context.Context, f localFile) error {
	now, err := os.Stat(f.path)
	if err != nil {
		// gone or unreadable: the object is what was read, keep it
		return fmt.Errorf("check local file after upload: %w", err)
	}
	if sameVersion(f.info, now) {
		return nil
	}
	// the delete runs even after a first ctrl-c
	err = r.call(context.WithoutCancel(ctx), listTimeout, func(ctx context.Context) error {
		_, err := r.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(r.ID), Key: aws.String(f.key)})
		return err
	})
	if err != nil {
		return refusef("%s changed during upload and the remote copy could not be removed (%v); rerun with --force", f.path, err)
	}
	return errLocalChanged
}

func partSizeFor(size int64) (int64, error) {
	partSize := minPartSize
	if need := (size + maxParts - 1) / maxParts; need > partSize {
		// round up to a whole mib
		partSize = (need + (1<<20 - 1)) &^ (1<<20 - 1)
	}
	if partSize > maxPartSize {
		return 0, refusef("file too large: %d bytes needs parts over the 500 mb limit", size)
	}
	return partSize, nil
}

func partCount(size, partSize int64) int32 {
	return int32((size + partSize - 1) / partSize)
}

// partRange is the offset and length of 1-based part n.
func partRange(n int32, size, partSize int64) (int64, int64) {
	off := int64(n-1) * partSize
	return off, min(partSize, size-off)
}

var md5Hex = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// multipart is one multipart upload of a local file.
type multipart struct {
	*run
	file      *os.File
	f         localFile
	size      int64
	partSize  int64
	n         int32
	statePath string
	state     uploadState
	etags     map[int32]string // by part number, filled as parts land
	mu        sync.Mutex
}

func (r *run) multipartUpload(ctx context.Context, file *os.File, f localFile) error {
	size := f.info.Size()
	partSize, err := partSizeFor(size)
	if err != nil {
		return err
	}
	statePath, err := uploadStatePath(r.Endpoint, r.ID, f.key, f.path)
	if err != nil {
		return err
	}
	m := &multipart{
		run: r, file: file, f: f, size: size, partSize: partSize,
		n: partCount(size, partSize), statePath: statePath, etags: map[int32]string{},
	}
	resumed, err := m.resume(ctx)
	if err != nil {
		return err
	}
	if !resumed {
		if err := m.start(ctx); err != nil {
			return err
		}
	}
	if err := m.sendMissing(ctx); err != nil {
		return err
	}
	// the parts were read over the whole upload; if the file moved under
	// them, they may mix two versions of it
	if localChanged(f) {
		m.abandon(ctx)
		return errLocalChanged
	}
	return m.complete(ctx)
}

// resume picks up the upload a saved state file points at, keeping the
// parts whose content still matches. It reports false when there is
// nothing to resume.
func (m *multipart) resume(ctx context.Context) (bool, error) {
	found, err := readJSON(m.statePath, &m.state)
	if err != nil || !found {
		return false, err
	}
	if !m.stateMatchesFile() {
		// the local file changed since the upload began: its parts are stale
		m.abort(ctx, m.f.key, m.state.UploadID)
		return false, nil
	}
	existing, err := m.listParts(ctx, m.f.key, m.state.UploadID)
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("list uploaded parts: %w", err)
	}
	m.etags, err = verifyParts(m.file, existing, m.n, m.size, m.partSize)
	return err == nil, err
}

func (m *multipart) stateMatchesFile() bool {
	return m.state.FileSize == m.size &&
		m.state.MtimeNs == m.f.info.ModTime().UnixNano() &&
		m.state.PartSize == m.partSize
}

// start creates a new upload and saves its state for a rerun.
func (m *multipart) start(ctx context.Context) error {
	var out *s3.CreateMultipartUploadOutput
	err := m.call(ctx, listTimeout, func(ctx context.Context) error {
		var err error
		out, err = m.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
			Bucket: aws.String(m.ID),
			Key:    aws.String(m.f.key),
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("start multipart upload: %w", err)
	}
	m.state = uploadState{UploadID: aws.ToString(out.UploadId), PartSize: m.partSize, FileSize: m.size, MtimeNs: m.f.info.ModTime().UnixNano()}
	m.etags = map[int32]string{}
	if err := writeJSON(m.statePath, m.state); err != nil {
		// without saved state no rerun can find this upload again
		m.abort(ctx, m.f.key, m.state.UploadID)
		return fmt.Errorf("save upload state: %w", err)
	}
	return nil
}

// sendMissing uploads every part the server does not already hold.
func (m *multipart) sendMissing(ctx context.Context) error {
	var missing []int32
	for i := int32(1); i <= m.n; i++ {
		if _, ok := m.etags[i]; ok {
			_, length := partRange(i, m.size, m.partSize)
			m.Reporter.Reused(length)
			continue
		}
		missing = append(missing, i)
	}
	return forEach(ctx, m.Concurrency, missing, m.sendPart)
}

func (m *multipart) sendPart(ctx context.Context, part int32) error {
	off, length := partRange(part, m.size, m.partSize)
	var etag string
	err := m.call(ctx, requestTimeout, func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		body := newIdleSectionReader(io.NewSectionReader(m.file, off, length), idleTimeout, cancel)
		defer body.stop()
		out, err := m.Client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:        aws.String(m.ID),
			Key:           aws.String(m.f.key),
			UploadId:      aws.String(m.state.UploadID),
			PartNumber:    aws.Int32(part),
			Body:          body,
			ContentLength: aws.Int64(length),
		})
		if err == nil {
			etag = aws.ToString(out.ETag)
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("part %d/%d: %w", part, m.n, err)
	}
	m.mu.Lock()
	m.etags[part] = etag
	m.mu.Unlock()
	m.addBytes(length)
	return nil
}

// complete assembles the parts into the object and drops the state file.
func (m *multipart) complete(ctx context.Context) error {
	err := m.call(ctx, completeTimeout, func(ctx context.Context) error {
		_, err := m.Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
			Bucket:          aws.String(m.ID),
			Key:             aws.String(m.f.key),
			UploadId:        aws.String(m.state.UploadID),
			MultipartUpload: &types.CompletedMultipartUpload{Parts: completedParts(m.etags, m.n)},
		})
		return err
	})
	if err != nil && !m.completedAnyway(ctx, err, m.f.key, m.size, multipartETag(m.etags, m.n)) {
		return fmt.Errorf("complete multipart upload: %w", err)
	}
	_ = os.Remove(m.statePath)
	return nil
}

// abandon aborts the upload and forgets it.
func (m *multipart) abandon(ctx context.Context) {
	m.abort(ctx, m.f.key, m.state.UploadID)
	_ = os.Remove(m.statePath)
}

func (r *run) listParts(ctx context.Context, key, uploadID string) ([]types.Part, error) {
	var parts []types.Part
	var marker *string
	for {
		var out *s3.ListPartsOutput
		err := r.call(ctx, listTimeout, func(ctx context.Context) error {
			var err error
			out, err = r.Client.ListParts(ctx, &s3.ListPartsInput{
				Bucket:           aws.String(r.ID),
				Key:              aws.String(key),
				UploadId:         aws.String(uploadID),
				PartNumberMarker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		parts = append(parts, out.Parts...)
		if !aws.ToBool(out.IsTruncated) || out.NextPartNumberMarker == nil || len(out.Parts) == 0 {
			return parts, nil
		}
		if marker != nil && *marker == *out.NextPartNumberMarker {
			return nil, errors.New("part listing returned the same page marker twice; retry shortly")
		}
		marker = out.NextPartNumberMarker
	}
}

// remoteFolderExists reports whether key is a folder on the volume: something is
// listed under key + "/" (runpod lists even an empty folder's own marker).
func (r *run) remoteFolderExists(ctx context.Context, key string) bool {
	var out *s3.ListObjectsV2Output
	err := r.call(ctx, listTimeout, func(ctx context.Context) error {
		var err error
		out, err = r.Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:  aws.String(r.ID),
			Prefix:  aws.String(key + "/"),
			MaxKeys: aws.Int32(1),
		})
		return err
	})
	return err == nil && len(out.Contents) > 0
}

// errLocalChanged is a local file modified while it was uploading. Nothing of
// it is kept, so a rerun uploads the new version.
var errLocalChanged = errors.New("local file changed during upload")

// localChanged reports whether f no longer has the size and mtime it was
// measured with. Retries and parts reread the file, so a change can mix two
// versions.
func localChanged(f localFile) bool {
	now, err := os.Stat(f.path)
	return err != nil || !sameVersion(f.info, now)
}

// sameVersion reports whether two stats of a file show the same size and
// modification time.
func sameVersion(a, b os.FileInfo) bool {
	return a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// isDownloadLeftover reports a partial download, its sidecar, or the
// sidecar's temp file, which an interrupted download leaves next to the
// target and which are not data to upload.
func isDownloadLeftover(path string) bool {
	return strings.HasSuffix(path, partialSuffix) || strings.HasSuffix(path, sidecarSuffix) || strings.HasSuffix(path, sidecarSuffix+".tmp")
}

// completedAnyway reports whether a failed complete actually took effect.
// Assembling a large file can outlive the proxy (524) while it still finishes
// server-side, and a retry then finds the upload gone. Only those ambiguous
// failures qualify, and only when the object at the key is this upload's:
// its size matches and its etag equals the multipart etag of our parts.
func (r *run) completedAnyway(ctx context.Context, err error, key string, size int64, wantETag string) bool {
	if !transient(err) && !isNotFound(err) {
		return false
	}
	obj, headErr := r.headObject(ctx, key)
	if headErr != nil || obj == nil || obj.size != size {
		return false
	}
	// without our multipart etag the object's identity cannot be confirmed
	return wantETag != "" && obj.etag == wantETag
}

// multipartETag is the etag s3 (and runpod, verified) gives an object
// assembled from parts: the md5 of the parts' binary md5s, then "-<count>".
// It is "" when a part etag is not an md5.
func multipartETag(etags map[int32]string, n int32) string {
	h := md5.New()
	for i := int32(1); i <= n; i++ {
		sum, err := hex.DecodeString(trimETag(etags[i]))
		if err != nil || len(sum) != md5.Size {
			return ""
		}
		h.Write(sum)
	}
	return fmt.Sprintf("%s-%d", hex.EncodeToString(h.Sum(nil)), n)
}

// verifyParts keeps the server's parts that match the local file: right size,
// and an md5 etag equal to the md5 of the local bytes. A part whose content
// cannot be verified is uploaded again.
func verifyParts(file *os.File, parts []types.Part, n int32, size, partSize int64) (map[int32]string, error) {
	etags := map[int32]string{}
	for _, p := range parts {
		num := aws.ToInt32(p.PartNumber)
		if num < 1 || num > n {
			continue
		}
		off, length := partRange(num, size, partSize)
		if aws.ToInt64(p.Size) != length {
			continue
		}
		etag := aws.ToString(p.ETag)
		want := strings.ToLower(trimETag(etag))
		if !md5Hex.MatchString(want) {
			continue
		}
		h := md5.New()
		if _, err := io.Copy(h, io.NewSectionReader(file, off, length)); err != nil {
			return nil, err
		}
		if hex.EncodeToString(h.Sum(nil)) != want {
			continue
		}
		etags[num] = etag
	}
	return etags, nil
}

// abort discards an upload that can no longer be resumed. Failure only leaves
// orphaned parts behind, so it is not an error.
func (r *run) abort(ctx context.Context, key, uploadID string) {
	_ = r.call(ctx, listTimeout, func(ctx context.Context) error {
		_, err := r.Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(r.ID),
			Key:      aws.String(key),
			UploadId: aws.String(uploadID),
		})
		if isNotFound(err) {
			return nil
		}
		return err
	})
}
