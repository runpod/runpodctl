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
	onGetObject  func(rng string) error
	// onComplete runs after the object is assembled, so it can simulate a
	// completion that succeeds server-side but fails on the wire
	onComplete func() error
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string]*fakeObject{}, uploads: map[string]*fakeUpload{}, calls: map[string]int{}}
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
		ETag:          aws.String(etagOf(obj.data)),
		LastModified:  aws.Time(obj.modified),
	}, nil
}

func (f *fakeS3) GetObject(ctx context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
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
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: aws.Int64(int64(len(data))),
		ETag:          aws.String(etagOf(obj.data)),
		LastModified:  aws.Time(obj.modified),
	}, nil
}

func (f *fakeS3) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
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
			ETag:         aws.String(etagOf(obj.data)),
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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["CreateMultipartUpload"]++
	f.nextID++
	id := fmt.Sprintf("upload-%d", f.nextID)
	f.uploads[id] = &fakeUpload{key: *in.Key, parts: map[int32][]byte{}}
	return &s3.CreateMultipartUploadOutput{UploadId: aws.String(id)}, nil
}

func (f *fakeS3) UploadPart(ctx context.Context, in *s3.UploadPartInput, _ ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
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
	return &s3.UploadPartOutput{ETag: aws.String(etagOf(data))}, nil
}

func (f *fakeS3) ListParts(ctx context.Context, in *s3.ListPartsInput, _ ...func(*s3.Options)) (*s3.ListPartsOutput, error) {
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
	out := &s3.ListPartsOutput{}
	for _, n := range nums {
		if n <= marker {
			continue
		}
		data := up.parts[int32(n)]
		out.Parts = append(out.Parts, types.Part{PartNumber: aws.Int32(int32(n)), Size: aws.Int64(int64(len(data))), ETag: aws.String(etagOf(data))})
	}
	return out, nil
}

func (f *fakeS3) CompleteMultipartUpload(ctx context.Context, in *s3.CompleteMultipartUploadInput, _ ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	f.mu.Lock()
	f.calls["CompleteMultipartUpload"]++
	up, ok := f.uploads[*in.UploadId]
	if !ok {
		f.mu.Unlock()
		return nil, apiError("NoSuchUpload")
	}
	var buf bytes.Buffer
	for i, p := range in.MultipartUpload.Parts {
		if int(*p.PartNumber) != i+1 {
			f.mu.Unlock()
			return nil, errors.New("parts out of order")
		}
		data, ok := up.parts[*p.PartNumber]
		if !ok || etagOf(data) != *p.ETag {
			f.mu.Unlock()
			return nil, apiError("InvalidPart")
		}
		buf.Write(data)
	}
	f.objects[up.key] = &fakeObject{data: buf.Bytes(), modified: time.Now()}
	delete(f.uploads, *in.UploadId)
	f.mu.Unlock()
	if f.onComplete != nil {
		if err := f.onComplete(); err != nil {
			return nil, err
		}
	}
	return &s3.CompleteMultipartUploadOutput{}, nil
}

func (f *fakeS3) AbortMultipartUpload(ctx context.Context, in *s3.AbortMultipartUploadInput, _ ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls["AbortMultipartUpload"]++
	if _, ok := f.uploads[*in.UploadId]; !ok {
		return nil, apiError("NoSuchUpload")
	}
	delete(f.uploads, *in.UploadId)
	return &s3.AbortMultipartUploadOutput{}, nil
}
