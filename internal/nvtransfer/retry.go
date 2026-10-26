package nvtransfer

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
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
// runs out of attempts. Each attempt gets its own timeout so a stalled
// connection is abandoned instead of hanging the transfer.
func withRetry(ctx context.Context, attemptTimeout time.Duration, fn func(ctx context.Context) error) error {
	var err error
	delay := retryPolicy.baseDelay
	for attempt := 1; attempt <= retryPolicy.attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		err = fn(attemptCtx)
		cancel()
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
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *smithy.OperationError
	// an operation error without an http status never reached the server, or
	// lost the connection mid-response
	return errors.As(err, &opErr)
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
