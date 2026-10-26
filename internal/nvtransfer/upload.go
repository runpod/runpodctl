package nvtransfer

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
func Upload(ctx context.Context, opts Options, localPath, remotePath string) (*Summary, error) {
	r := newRun(opts)
	abs, err := filepath.Abs(localPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if remotePath == "" {
		remotePath = filepath.Base(abs)
	}
	key, err := cleanKey(remotePath)
	if err != nil {
		return nil, err
	}

	var todo []localFile
	var total int64
	if info.IsDir() {
		files, err := walkLocal(abs, key)
		if err != nil {
			return nil, err
		}
		prefix := key
		if prefix != "" && prefix[len(prefix)-1] != '/' {
			prefix += "/"
		}
		remote, err := r.listPrefix(ctx, prefix)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if obj, ok := remote[f.key]; ok && unchanged(f.info, obj) {
				r.skipped()
				continue
			}
			todo = append(todo, f)
			total += f.info.Size()
		}
	} else {
		if key == "" || key[len(key)-1] == '/' {
			key += filepath.Base(abs)
		}
		obj, err := r.headObject(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("check %q: %w", key, err)
		}
		if obj != nil && unchanged(info, *obj) {
			r.skipped()
		} else {
			todo = append(todo, localFile{path: abs, key: key, info: info})
			total = info.Size()
		}
	}

	r.Reporter.Start(len(todo), total)
	err = forEach(ctx, r.Concurrency, todo, func(ctx context.Context, f localFile) error {
		if err := r.uploadFile(ctx, f); err != nil {
			return fmt.Errorf("upload %s: %w", f.key, err)
		}
		r.transferred(f.key, f.info.Size())
		return nil
	})
	return &r.summary, err
}

// unchanged is the skip rule: same size, and the remote copy was written at or
// after the local file's last change.
func unchanged(local os.FileInfo, remote remoteObject) bool {
	return local.Size() == remote.size && !remote.modified.Before(local.ModTime())
}

// walkLocal lists the regular files under root (following symlinked files)
// with their keys under prefix.
func walkLocal(root, prefix string) ([]localFile, error) {
	var files []localFile
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
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, localFile{path: p, key: joinKey(prefix, filepath.ToSlash(rel)), info: info})
		return nil
	})
	return files, err
}

func (r *run) uploadFile(ctx context.Context, f localFile) error {
	file, err := os.Open(f.path)
	if err != nil {
		return err
	}
	defer file.Close()
	size := f.info.Size()

	if size <= singleRequestLimit {
		err := withRetry(ctx, requestTimeout, func(ctx context.Context) error {
			return r.request(ctx, func() error {
				_, err := r.Client.PutObject(ctx, &s3.PutObjectInput{
					Bucket:        aws.String(r.VolumeID),
					Key:           aws.String(f.key),
					Body:          io.NewSectionReader(file, 0, size),
					ContentLength: aws.Int64(size),
				})
				return err
			})
		})
		if err == nil {
			r.Reporter.Add(size)
		}
		return err
	}
	return r.multipartUpload(ctx, file, f)
}

func partSizeFor(size int64) (int64, error) {
	partSize := minPartSize
	if need := (size + maxParts - 1) / maxParts; need > partSize {
		// round up to a whole mib
		partSize = (need + (1<<20 - 1)) &^ (1<<20 - 1)
	}
	if partSize > maxPartSize {
		return 0, fmt.Errorf("file too large: %d bytes needs parts over the 500 mb limit", size)
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

func (r *run) multipartUpload(ctx context.Context, file *os.File, f localFile) error {
	size := f.info.Size()
	partSize, err := partSizeFor(size)
	if err != nil {
		return err
	}
	n := partCount(size, partSize)
	statePath, err := uploadStatePath(r.Endpoint, r.VolumeID, f.key, f.path)
	if err != nil {
		return err
	}

	etags := map[int32]string{}
	var st uploadState
	resumed, err := readJSON(statePath, &st)
	if err != nil {
		return err
	}
	if resumed && (st.FileSize != size || st.MtimeNs != f.info.ModTime().UnixNano() || st.PartSize != partSize) {
		// the local file changed since the upload began: its parts are stale
		r.abort(ctx, f.key, st.UploadID)
		resumed = false
	}
	if resumed {
		existing, err := r.listParts(ctx, f.key, st.UploadID)
		switch {
		case isNotFound(err):
			resumed = false
		case err != nil:
			return fmt.Errorf("list uploaded parts: %w", err)
		default:
			etags, err = verifyParts(file, existing, n, size, partSize)
			if err != nil {
				return err
			}
		}
	}
	if !resumed {
		var out *s3.CreateMultipartUploadOutput
		err := withRetry(ctx, requestTimeout, func(ctx context.Context) error {
			var err error
			out, err = r.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
				Bucket: aws.String(r.VolumeID),
				Key:    aws.String(f.key),
			})
			return err
		})
		if err != nil {
			return fmt.Errorf("start multipart upload: %w", err)
		}
		st = uploadState{UploadID: aws.ToString(out.UploadId), PartSize: partSize, FileSize: size, MtimeNs: f.info.ModTime().UnixNano()}
		if err := writeJSON(statePath, st); err != nil {
			// without saved state no rerun can find this upload again
			r.abort(ctx, f.key, st.UploadID)
			return fmt.Errorf("save upload state: %w", err)
		}
	}

	var missing []int32
	for i := int32(1); i <= n; i++ {
		if _, ok := etags[i]; ok {
			_, length := partRange(i, size, partSize)
			r.Reporter.Add(length)
			continue
		}
		missing = append(missing, i)
	}

	var mu sync.Mutex
	err = forEach(ctx, r.Concurrency, missing, func(ctx context.Context, part int32) error {
		off, length := partRange(part, size, partSize)
		var etag string
		err := withRetry(ctx, requestTimeout, func(ctx context.Context) error {
			return r.request(ctx, func() error {
				out, err := r.Client.UploadPart(ctx, &s3.UploadPartInput{
					Bucket:        aws.String(r.VolumeID),
					Key:           aws.String(f.key),
					UploadId:      aws.String(st.UploadID),
					PartNumber:    aws.Int32(part),
					Body:          io.NewSectionReader(file, off, length),
					ContentLength: aws.Int64(length),
				})
				if err == nil {
					etag = aws.ToString(out.ETag)
				}
				return err
			})
		})
		if err != nil {
			return fmt.Errorf("part %d/%d: %w", part, n, err)
		}
		mu.Lock()
		etags[part] = etag
		mu.Unlock()
		r.Reporter.Add(length)
		return nil
	})
	if err != nil {
		return err
	}

	// the parts were read over the whole upload; if the file moved under
	// them, they may mix two versions of it
	if now, err := os.Stat(f.path); err != nil || now.Size() != size || !now.ModTime().Equal(f.info.ModTime()) {
		r.abort(ctx, f.key, st.UploadID)
		_ = os.Remove(statePath)
		return errLocalChanged
	}

	err = withRetry(ctx, completeTimeout, func(ctx context.Context) error {
		_, err := r.Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
			Bucket:          aws.String(r.VolumeID),
			Key:             aws.String(f.key),
			UploadId:        aws.String(st.UploadID),
			MultipartUpload: &types.CompletedMultipartUpload{Parts: completedParts(etags, n)},
		})
		return err
	})
	if err != nil && !r.completedAnyway(ctx, err, f.key, size, multipartETag(etags, n)) {
		return fmt.Errorf("complete multipart upload: %w", err)
	}
	_ = os.Remove(statePath)
	return nil
}

func (r *run) listParts(ctx context.Context, key, uploadID string) ([]types.Part, error) {
	var parts []types.Part
	var marker *string
	for {
		var out *s3.ListPartsOutput
		err := withRetry(ctx, listTimeout, func(ctx context.Context) error {
			var err error
			out, err = r.Client.ListParts(ctx, &s3.ListPartsInput{
				Bucket:           aws.String(r.VolumeID),
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
		marker = out.NextPartNumberMarker
	}
}

// errLocalChanged is a local file modified while its parts were uploading.
var errLocalChanged = errors.New("local file changed during upload; run the command again to upload the new version")

// completedAnyway reports whether a failed complete actually took effect.
// Assembling a large file can outlive the proxy (524) while it still finishes
// server-side, and a retry then finds the upload gone. Only those ambiguous
// failures qualify, and only when the object at the key is this upload's:
// its etag equals the multipart etag of our parts, or, if that cannot be
// computed, its size matches.
func (r *run) completedAnyway(ctx context.Context, err error, key string, size int64, wantETag string) bool {
	if !retryable(err) && !isNotFound(err) {
		return false
	}
	obj, headErr := r.headObject(ctx, key)
	if headErr != nil || obj == nil || obj.size != size {
		return false
	}
	return wantETag == "" || obj.etag == wantETag
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
	_ = withRetry(ctx, listTimeout, func(ctx context.Context) error {
		_, err := r.Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(r.VolumeID),
			Key:      aws.String(key),
			UploadId: aws.String(uploadID),
		})
		if isNotFound(err) {
			return nil
		}
		return err
	})
}
