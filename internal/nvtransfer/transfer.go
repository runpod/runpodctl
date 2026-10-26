package nvtransfer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// sizes are variables so tests can transfer small files in many pieces.
var (
	// files up to this size go up in one put and come down in one get
	singleRequestLimit int64 = 64 << 20
	minPartSize        int64 = 64 << 20
	chunkSize          int64 = 64 << 20
)

const (
	// runpod rejects multipart parts larger than 500 mb
	maxPartSize = 500 << 20
	maxParts    = 10000

	// a request or response body that moves nothing for this long is
	// abandoned and retried, however slow the link
	idleTimeout = 2 * time.Minute
	// the cap on one attempt at a part or chunk; stalls are caught sooner by
	// idleTimeout, so this only bounds a link too slow to be useful
	requestTimeout = 30 * time.Minute
	// runpod's proxy answers a slow complete with 524 after about 100s, so
	// an attempt never legitimately needs longer than this
	completeTimeout = 30 * time.Minute
	listTimeout     = 10 * time.Minute

	// multipart parts and their metadata live here until the upload completes
	uploadsDir = ".s3compat_uploads/"
)

// Volume is a network volume reached through its s3 api, with the settings
// for transfers to and from it.
type Volume struct {
	Client S3API
	// Endpoint identifies the s3 endpoint in upload state file names, so the
	// same key on two data centers never shares state.
	Endpoint string
	ID       string
	// Concurrency bounds the requests in flight across all files. Default 4.
	Concurrency int
	// Force transfers every file, even those the skip rule calls unchanged.
	Force    bool
	Reporter Reporter
}

// RefusedError is a transfer the cli declines, for a reason a rerun would hit
// again: a name collision, a file too large, an upload onto a folder.
type RefusedError struct{ Err error }

func (e *RefusedError) Error() string { return e.Err.Error() }
func (e *RefusedError) Unwrap() error { return e.Err }

func refusef(format string, args ...any) error {
	return &RefusedError{Err: fmt.Errorf(format, args...)}
}

// Summary is the result of a transfer.
type Summary struct {
	FilesTransferred int   `json:"filesTransferred"`
	FilesSkipped     int   `json:"filesSkipped"`
	BytesTransferred int64 `json:"bytesTransferred"`
}

// Reporter receives progress. Calls may come from several goroutines.
type Reporter interface {
	// Start is called once the work is known: files and bytes to transfer,
	// excluding files skipped as unchanged.
	Start(files int, bytes int64)
	// Add reports bytes sent or received.
	Add(bytes int64)
	// Reused reports bytes an earlier run already moved: uploaded parts or
	// downloaded chunks a resume keeps.
	Reused(bytes int64)
	FileDone(name string)
}

type nopReporter struct{}

func (nopReporter) Start(int, int64) {}
func (nopReporter) Add(int64)        {}
func (nopReporter) Reused(int64)     {}
func (nopReporter) FileDone(string)  {}

// run is one transfer's shared state.
type run struct {
	Volume
	sem chan struct{}

	mu      sync.Mutex
	summary Summary
}

func newRun(v Volume) *run {
	if v.Concurrency <= 0 {
		v.Concurrency = 4
	}
	if v.Reporter == nil {
		v.Reporter = nopReporter{}
	}
	return &run{Volume: v, sem: make(chan struct{}, v.Concurrency)}
}

// call is a retried request holding a concurrency slot per attempt. The
// attempt's deadline starts once the slot is held, so time spent queued behind
// other requests does not count against it.
func (r *run) call(ctx context.Context, timeout time.Duration, fn func(ctx context.Context) error) error {
	return withRetry(ctx, func(ctx context.Context) error {
		return r.request(ctx, func() error {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return fn(ctx)
		})
	})
}

// request holds a concurrency slot for the duration of fn.
func (r *run) request(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-r.sem }()
	return fn()
}

func (r *run) transferred(name string) {
	r.mu.Lock()
	r.summary.FilesTransferred++
	r.mu.Unlock()
	r.Reporter.FileDone(name)
}

// addBytes counts bytes this run moved; reused ones are not counted.
func (r *run) addBytes(n int64) {
	r.mu.Lock()
	r.summary.BytesTransferred += n
	r.mu.Unlock()
	r.Reporter.Add(n)
}

// remoteUnchanged is the upload skip rule: same size, and the remote copy was
// written at or after the local file's last change. downloadUnchanged is the
// mirror for downloads, which stamp the remote time on the file. Times are
// compared in whole seconds: s3 dates carry no finer precision.
func remoteUnchanged(local os.FileInfo, remote remoteObject) bool {
	return local.Size() == remote.size && !remote.modified.Truncate(time.Second).Before(local.ModTime().Truncate(time.Second))
}

func downloadUnchanged(local os.FileInfo, remote remoteObject) bool {
	return local.Size() == remote.size && !remote.modified.Truncate(time.Second).After(local.ModTime().Truncate(time.Second))
}

func (r *run) skipped() {
	r.mu.Lock()
	r.summary.FilesSkipped++
	r.mu.Unlock()
}

// feed sends items to work until they run out or ctx is done, then closes it.
func feed[T any](ctx context.Context, work chan<- T, items []T) {
	defer close(work)
	for _, item := range items {
		select {
		case work <- item:
		case <-ctx.Done():
			return
		}
	}
}

// forEach runs fn over items with up to n at once and returns the first error,
// canceling the rest.
func forEach[T any](ctx context.Context, n int, items []T, fn func(ctx context.Context, item T) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	work := make(chan T)
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	for i := 0; i < n && i < len(items); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				// select picks at random between feeding and cancellation, so
				// an item can still arrive after the first failure
				if ctx.Err() != nil {
					continue
				}
				if err := fn(ctx, item); err != nil {
					once.Do(func() { firstErr = err; cancel() })
				}
			}
		}()
	}
	feed(ctx, work, items)
	wg.Wait()
	if firstErr == nil && ctx.Err() != nil {
		// the parent was canceled
		return context.Cause(ctx)
	}
	return firstErr
}

// cleanKey normalizes a volume path to an object key: forward slashes, no
// leading slash, no dot segments. A trailing slash is kept, since it marks a
// directory. The volume root is "".
func cleanKey(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	dir := strings.HasSuffix(p, "/")
	p = strings.TrimLeft(p, "/")
	if p == "" || p == "." {
		return "", nil
	}
	for _, seg := range strings.Split(strings.TrimSuffix(p, "/"), "/") {
		if seg == ".." {
			return "", fmt.Errorf("remote path %q must not contain '..': %w", p, ErrInvalidPath)
		}
	}
	p = path.Clean(p)
	if p == "." {
		return "", nil
	}
	if dir {
		p += "/"
	}
	return p, nil
}

// ErrInvalidPath is a remote path the transfer refuses before starting.
var ErrInvalidPath = errors.New("invalid path")

func joinKey(prefix, rel string) string {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return rel
	}
	return prefix + "/" + rel
}

type remoteObject struct {
	size     int64
	modified time.Time
	etag     string
}

// listPrefix lists every object under prefix (which ends in "/" or is "").
// Directory markers and in-progress multipart data are left out.
// addFiles adds the listed files to objects, leaving out folder markers and
// in-progress multipart parts.
func addFiles(objects map[string]remoteObject, listed []types.Object) {
	for _, obj := range listed {
		key := aws.ToString(obj.Key)
		if strings.HasSuffix(key, "/") || strings.HasPrefix(key, uploadsDir) || strings.Contains(key, "/"+uploadsDir) {
			continue
		}
		objects[key] = remoteObject{
			size:     aws.ToInt64(obj.Size),
			modified: aws.ToTime(obj.LastModified),
			etag:     trimETag(aws.ToString(obj.ETag)),
		}
	}
}

func (r *run) listPrefix(ctx context.Context, prefix string) (map[string]remoteObject, error) {
	objects := map[string]remoteObject{}
	var token *string
	for {
		var out *s3.ListObjectsV2Output
		err := r.call(ctx, listTimeout, func(ctx context.Context) error {
			var err error
			out, err = r.Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
				Bucket:            aws.String(r.ID),
				Prefix:            aws.String(prefix),
				ContinuationToken: token,
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("list %q: %w", prefix, err)
		}
		addFiles(objects, out.Contents)
		if !aws.ToBool(out.IsTruncated) || out.NextContinuationToken == nil {
			return objects, nil
		}
		if token != nil && *token == *out.NextContinuationToken {
			// runpod computes md5s for files written outside the s3 api on
			// first listing, and can hand back the same token meanwhile
			return nil, errors.New("listing returned the same page token twice; the volume may still be indexing files, retry shortly")
		}
		token = out.NextContinuationToken
	}
}

// headObject returns nil, nil when the object does not exist.
func (r *run) headObject(ctx context.Context, key string) (*remoteObject, error) {
	var out *s3.HeadObjectOutput
	err := r.call(ctx, listTimeout, func(ctx context.Context) error {
		var err error
		out, err = r.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(r.ID), Key: aws.String(key)})
		return err
	})
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &remoteObject{
		size:     aws.ToInt64(out.ContentLength),
		modified: aws.ToTime(out.LastModified),
		etag:     trimETag(aws.ToString(out.ETag)),
	}, nil
}

func trimETag(etag string) string { return strings.Trim(etag, `"`) }

// completedParts lists parts 1..n with the etags exactly as the server sent them.
func completedParts(etags map[int32]string, n int32) []types.CompletedPart {
	parts := make([]types.CompletedPart, 0, n)
	for i := int32(1); i <= n; i++ {
		parts = append(parts, types.CompletedPart{PartNumber: aws.Int32(i), ETag: aws.String(etags[i])})
	}
	return parts
}
