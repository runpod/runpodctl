package nvtransfer

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"syscall"
	"time"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	defaultMaxAttempts = 8
	defaultBaseDelay   = time.Second
	maxDelay           = 30 * time.Second
)

// retryPolicy is package state so tests can make retries instant.
var retryPolicy = struct {
	attempts  int
	baseDelay time.Duration
}{defaultMaxAttempts, defaultBaseDelay}

// withRetry runs fn until it succeeds, fails with a non-retryable error, or
// runs out of attempts. fn bounds each attempt itself (see run.call).
func withRetry(ctx context.Context, fn func(ctx context.Context) error) error {
	var err error
	delay := retryPolicy.baseDelay
	for attempt := 1; attempt <= retryPolicy.attempts; attempt++ {
		err = fn(ctx)
		if err == nil || ctx.Err() != nil || !retryable(err) || attempt == retryPolicy.attempts {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
	return err
}

// retryable reports whether err is worth another attempt: network failures,
// timeouts, throttling and 5xx (runpod returns 524 when an operation outlives
// its proxy). Other 4xx are final.
func retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var transient transientError
	if errors.As(err, &transient) {
		return true
	}
	if status := HTTPStatus(err); status != 0 {
		return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
	}
	return transportFailed(err)
}

// transportFailed reports an error with no http status that a retry can
// get past: a dropped or reset connection, not a local or sdk failure.
func transportFailed(err error) bool {
	// a local filesystem failure (a full disk, permissions) does not pass on
	// its own; checked before net.Error, which syscall.Errno also satisfies
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// a request that failed in transit, as opposed to one the sdk refused to
	// build (bad parameters, endpoint resolution), which never succeeds
	var sendErr *smithyhttp.RequestSendError
	return errors.As(err, &sendErr) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET)
}

// Resumable reports a final-looking s3 error that a rerun gets past: the
// multipart upload vanished (NoSuchUpload, e.g. after a complete whose 524
// hid that it finished), or a range that no longer fits an object that
// changed mid-download (416).
func Resumable(err error) bool {
	if HTTPStatus(err) == http.StatusRequestedRangeNotSatisfiable {
		return true
	}
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchUpload"
}

// HTTPStatus is the s3 response status behind err, or 0 when it never got one.
func HTTPStatus(err error) int {
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		return respErr.HTTPStatusCode()
	}
	return 0
}

// isNotFound reports a 404 or a NoSuchKey/NoSuchUpload error code.
func isNotFound(err error) bool {
	if HTTPStatus(err) == http.StatusNotFound {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NoSuchUpload", "NotFound":
			return true
		}
	}
	return false
}
