package nvtransfer

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type fakeObject struct {
	data     []byte
	modified time.Time
	// etag is set for multipart objects, which get "<md5 of md5s>-<n>"
	etag string
}

func (o *fakeObject) etagValue() string {
	if o.etag != "" {
		return o.etag
	}
	return etagOf(o.data)
}

type fakeUpload struct {
	key   string
	parts map[int32][]byte
}

// fakeS3 is an in-memory s3 with hooks to inject failures. Hooks run before
// the operation and may return an error to fail it.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]*fakeObject
	uploads map[string]*fakeUpload
	nextID  int
	calls   map[string]int

	onUploadPart func(part int32) error
	// onCompleteBefore runs before the object is assembled
	onCompleteBefore func() error
	// opaquePartETags makes parts report etags that are not md5s
	opaquePartETags bool

	// every call counts toward inflight; peak is the most at once
	inflight, peak atomic.Int32
	callDelay      time.Duration
	onGetObject    func(rng string) error
	// dropBody makes a GetObject body fail halfway when it returns true
	dropBody func(rng string) bool
	// onComplete runs after the object is assembled, so it can simulate a
	// completion that succeeds server-side but fails on the wire
	onComplete func() error
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string]*fakeObject{}, uploads: map[string]*fakeUpload{}, calls: map[string]int{}}
}

// track counts a call in flight until the returned func runs.
func (f *fakeS3) track() func() {
	n := f.inflight.Add(1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if f.callDelay > 0 {
		time.Sleep(f.callDelay)
	}
	return func() { f.inflight.Add(-1) }
}

func (f *fakeS3) count(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[op]
}

func (f *fakeS3) put(key string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = &fakeObject{data: data, modified: time.Now()}
}

func (f *fakeS3) get(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[key]
	if !ok {
		return nil, false
	}
	return obj.data, true
}

func (f *fakeS3) partETag(data []byte) string {
	if f.opaquePartETags {
		return fmt.Sprintf(`"opaque-%d"`, len(data))
	}
	return etagOf(data)
}

func etagOf(data []byte) string {
	sum := md5.Sum(data)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func statusError(code int) error {
	return &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: code}},
		Err:      fmt.Errorf("http %d", code),
	}}
}

func apiError(code string) error {
	return &smithy.GenericAPIError{Code: code, Message: code}
}

var errForbidden = statusError(http.StatusForbidden)

func (f *fakeS3) PutObject(ctx context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	defer f.track()()
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["PutObject"]++
	f.objects[*in.Key] = &fakeObject{data: data, modified: time.Now()}
	return &s3.PutObjectOutput{ETag: aws.String(etagOf(data))}, nil
}

func (f *fakeS3) HeadObject(ctx context.Context, in *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	defer f.track()()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["HeadObject"]++
	obj, ok := f.objects[*in.Key]
	if !ok {
		// like runpod, a head on a directory is a 400
		for k := range f.objects {
			if strings.HasPrefix(k, *in.Key+"/") {
				return nil, statusError(http.StatusBadRequest)
			}
		}
		return nil, statusError(http.StatusNotFound)
	}
	return &s3.HeadObjectOutput{
		ContentLength: aws.Int64(int64(len(obj.data))),
		ETag:          aws.String(obj.etagValue()),
		LastModified:  aws.Time(obj.modified),
	}, nil
}

func (f *fakeS3) GetObject(ctx context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	defer f.track()()
	rng := aws.ToString(in.Range)
	if f.onGetObject != nil {
		if err := f.onGetObject(rng); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["GetObject"]++
	obj, ok := f.objects[*in.Key]
	if !ok {
		return nil, apiError("NoSuchKey")
	}
	data := obj.data
	if rng != "" {
		var start, end int
		if _, err := fmt.Sscanf(rng, "bytes=%d-%d", &start, &end); err != nil {
			return nil, err
		}
		data = data[start : end+1]
	}
	var body io.Reader = bytes.NewReader(data)
	if f.dropBody != nil && f.dropBody(rng) {
		body = io.MultiReader(bytes.NewReader(data[:len(data)/2]), failingReader{io.ErrUnexpectedEOF})
	}
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(body),
		ContentLength: aws.Int64(int64(len(data))),
		ETag:          aws.String(obj.etagValue()),
		LastModified:  aws.Time(obj.modified),
	}, nil
}

func (f *fakeS3) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	defer f.track()()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["ListObjectsV2"]++
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, aws.ToString(in.Prefix)) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	// two keys per page to exercise pagination
	start := 0
	if in.ContinuationToken != nil {
		fmt.Sscanf(*in.ContinuationToken, "%d", &start)
	}
	end := min(start+2, len(keys))
	out := &s3.ListObjectsV2Output{}
	for _, k := range keys[start:end] {
		obj := f.objects[k]
		out.Contents = append(out.Contents, types.Object{
			Key:          aws.String(k),
			Size:         aws.Int64(int64(len(obj.data))),
			ETag:         aws.String(obj.etagValue()),
			LastModified: aws.Time(obj.modified),
		})
	}
	if end < len(keys) {
		out.IsTruncated = aws.Bool(true)
		out.NextContinuationToken = aws.String(fmt.Sprint(end))
	}
	return out, nil
}

func (f *fakeS3) CreateMultipartUpload(ctx context.Context, in *s3.CreateMultipartUploadInput, _ ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	defer f.track()()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["CreateMultipartUpload"]++
	f.nextID++
	id := fmt.Sprintf("upload-%d", f.nextID)
	f.uploads[id] = &fakeUpload{key: *in.Key, parts: map[int32][]byte{}}
	return &s3.CreateMultipartUploadOutput{UploadId: aws.String(id)}, nil
}

func (f *fakeS3) UploadPart(ctx context.Context, in *s3.UploadPartInput, _ ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
	defer f.track()()
	if f.onUploadPart != nil {
		if err := f.onUploadPart(*in.PartNumber); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["UploadPart"]++
	up, ok := f.uploads[*in.UploadId]
	if !ok {
		return nil, apiError("NoSuchUpload")
	}
	up.parts[*in.PartNumber] = data
	return &s3.UploadPartOutput{ETag: aws.String(f.partETag(data))}, nil
}

func (f *fakeS3) ListParts(ctx context.Context, in *s3.ListPartsInput, _ ...func(*s3.Options)) (*s3.ListPartsOutput, error) {
	defer f.track()()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["ListParts"]++
	up, ok := f.uploads[*in.UploadId]
	if !ok {
		return nil, apiError("NoSuchUpload")
	}
	var nums []int
	for n := range up.parts {
		nums = append(nums, int(n))
	}
	sort.Ints(nums)
	marker := 0
	if in.PartNumberMarker != nil {
		fmt.Sscanf(*in.PartNumberMarker, "%d", &marker)
	}
	// two parts per page to exercise pagination
	out := &s3.ListPartsOutput{}
	for _, n := range nums {
		if n <= marker {
			continue
		}
		if len(out.Parts) == 2 {
			out.IsTruncated = aws.Bool(true)
			out.NextPartNumberMarker = aws.String(fmt.Sprint(*out.Parts[1].PartNumber))
			break
		}
		data := up.parts[int32(n)]
		out.Parts = append(out.Parts, types.Part{PartNumber: aws.Int32(int32(n)), Size: aws.Int64(int64(len(data))), ETag: aws.String(f.partETag(data))})
	}
	return out, nil
}

func (f *fakeS3) CompleteMultipartUpload(ctx context.Context, in *s3.CompleteMultipartUploadInput, _ ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	defer f.track()()
	if f.onCompleteBefore != nil {
		if err := f.onCompleteBefore(); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	f.calls["CompleteMultipartUpload"]++
	up, ok := f.uploads[*in.UploadId]
	if !ok {
		f.mu.Unlock()
		return nil, apiError("NoSuchUpload")
	}
	var buf bytes.Buffer
	sums := md5.New()
	for i, p := range in.MultipartUpload.Parts {
		if int(*p.PartNumber) != i+1 {
			f.mu.Unlock()
			return nil, errors.New("parts out of order")
		}
		data, ok := up.parts[*p.PartNumber]
		if !ok || f.partETag(data) != *p.ETag {
			f.mu.Unlock()
			return nil, apiError("InvalidPart")
		}
		buf.Write(data)
		sum := md5.Sum(data)
		sums.Write(sum[:])
	}
	etag := fmt.Sprintf(`"%s-%d"`, hex.EncodeToString(sums.Sum(nil)), len(in.MultipartUpload.Parts))
	f.objects[up.key] = &fakeObject{data: buf.Bytes(), modified: time.Now(), etag: etag}
	delete(f.uploads, *in.UploadId)
	f.mu.Unlock()
	if f.onComplete != nil {
		if err := f.onComplete(); err != nil {
			return nil, err
		}
	}
	return &s3.CompleteMultipartUploadOutput{}, nil
}

func (f *fakeS3) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	defer f.track()()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["DeleteObject"]++
	delete(f.objects, *in.Key)
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeS3) AbortMultipartUpload(ctx context.Context, in *s3.AbortMultipartUploadInput, _ ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
	defer f.track()()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["AbortMultipartUpload"]++
	if _, ok := f.uploads[*in.UploadId]; !ok {
		return nil, apiError("NoSuchUpload")
	}
	delete(f.uploads, *in.UploadId)
	return &s3.AbortMultipartUploadOutput{}, nil
}

// failingReader fails every read with err.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
