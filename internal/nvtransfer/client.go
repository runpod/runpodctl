// Package nvtransfer moves files and folders between the local machine and a
// network volume through runpod's s3-compatible api. Every transfer can be
// interrupted and resumed by running it again: uploads keep their multipart
// upload id in a local state file, downloads keep finished chunks in a sidecar
// next to the partial file.
package nvtransfer

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3API is the subset of the s3 client a transfer uses. *s3.Client satisfies it.
type S3API interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(ctx context.Context, in *s3.HeadObjectInput, opts ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	CreateMultipartUpload(ctx context.Context, in *s3.CreateMultipartUploadInput, opts ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error)
	UploadPart(ctx context.Context, in *s3.UploadPartInput, opts ...func(*s3.Options)) (*s3.UploadPartOutput, error)
	ListParts(ctx context.Context, in *s3.ListPartsInput, opts ...func(*s3.Options)) (*s3.ListPartsOutput, error)
	CompleteMultipartUpload(ctx context.Context, in *s3.CompleteMultipartUploadInput, opts ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(ctx context.Context, in *s3.AbortMultipartUploadInput, opts ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// EndpointURL is the s3 endpoint for a data center, e.g. EU-RO-1 ->
// https://s3api-eu-ro-1.runpod.io/.
func EndpointURL(dataCenterID string) string {
	return "https://s3api-" + strings.ToLower(strings.TrimSpace(dataCenterID)) + ".runpod.io/"
}

// NewS3Client builds a client for one data center's endpoint. The sdk's own
// retries are off because every call goes through withRetry, which also covers
// the 524s runpod returns on slow operations. Checksums are only sent when an
// operation requires them: the sdk's default crc headers are not accepted by
// every s3-compatible store.
func NewS3Client(endpoint, region, accessKey, secretKey string) *s3.Client {
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 30 * time.Second,
		// runpod's proxy gives up on a slow response after about 100s (524),
		// so a server silent for longer than this is a dead connection
		ResponseHeaderTimeout: 5 * time.Minute,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return s3.New(s3.Options{
		Region: region,
		// a trailing slash would sign the path as "//bucket"
		BaseEndpoint:               aws.String(strings.TrimRight(endpoint, "/")),
		UsePathStyle:               true,
		Credentials:                credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		Retryer:                    aws.NopRetryer{},
		HTTPClient:                 &http.Client{Transport: transport},
		APIOptions:                 []func(*middleware.Stack) error{unsignedHeaders},
	})
}

// headersLeftUnsigned are sent but kept out of the sigv4 signature. runpod's
// endpoint rewrites Accept-Encoding before verifying, so signing it (the go
// sdk does, boto3 does not) fails every request with SignatureDoesNotMatch.
var headersLeftUnsigned = []string{"Accept-Encoding"}

type savedHeadersKey struct{}

// unsignedHeaders lifts headersLeftUnsigned off the request just before
// signing and puts them back right after.
func unsignedHeaders(stack *middleware.Stack) error {
	lift := middleware.FinalizeMiddlewareFunc("LiftUnsignedHeaders", func(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
		if req, ok := in.Request.(*smithyhttp.Request); ok {
			saved := http.Header{}
			for _, h := range headersLeftUnsigned {
				if v := req.Header.Values(h); len(v) > 0 {
					saved[h] = v
					req.Header.Del(h)
				}
			}
			ctx = middleware.WithStackValue(ctx, savedHeadersKey{}, saved)
		}
		return next.HandleFinalize(ctx, in)
	})
	restore := middleware.FinalizeMiddlewareFunc("RestoreUnsignedHeaders", func(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
		if req, ok := in.Request.(*smithyhttp.Request); ok {
			if saved, ok := middleware.GetStackValue(ctx, savedHeadersKey{}).(http.Header); ok {
				for h, v := range saved {
					req.Header[h] = v
				}
			}
		}
		return next.HandleFinalize(ctx, in)
	})
	if err := stack.Finalize.Insert(lift, "Signing", middleware.Before); err != nil {
		return err
	}
	return stack.Finalize.Insert(restore, "Signing", middleware.After)
}
