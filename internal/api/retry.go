package api

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// maxRetryAttempts bounds total tries (initial + retries) for a transient
// failure.
const maxRetryAttempts = 3

// maxRetryAfter caps how long a server's Retry-After can make the cli wait
// before one retry.
const maxRetryAfter = 10 * time.Second

// retryBackoff is the base delay for exponential backoff between retries. It is
// a var so tests can shorten it.
var retryBackoff = 500 * time.Millisecond

// idempotentMethod reports whether re-issuing a request with this method is safe
// even if the previous attempt may have reached the server. Only pure reads
// qualify: retrying a POST/PATCH could create or mutate a resource twice (e.g. a
// second billed pod), so those are never retried.
func idempotentMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// retryableStatus reports whether an HTTP status warrants a retry for the given
// method. Only idempotent reads are retried, on a 429 or a transient 5xx. A 429 does not
// prove a write did nothing: rest v2 pod create runs the create, then refetches
// the pod, and passes a 429 on that refetch through with the pod already made.
func retryableStatus(method string, statusCode int) bool {
	if !idempotentMethod(method) {
		return false
	}
	return statusCode == http.StatusTooManyRequests ||
		(statusCode >= 500 && statusCode < 600 && statusCode != http.StatusNotImplemented && statusCode != http.StatusHTTPVersionNotSupported)
}

// retryDelay is how long to wait before the given (1-based) attempt's retry.
// A nil header is fine. It honors a Retry-After header (seconds, capped at maxRetryAfter) when
// present, otherwise uses exponential backoff.
func retryDelay(header http.Header, attempt int) time.Duration {
	if secs, err := strconv.Atoi(header.Get("Retry-After")); err == nil && secs >= 0 {
		// cap before converting: a huge value overflows time.Duration negative.
		if secs >= int(maxRetryAfter/time.Second) {
			return maxRetryAfter
		}
		return time.Duration(secs) * time.Second
	}
	return retryBackoff * time.Duration(1<<(attempt-1))
}

// waitToRetry sleeps before retrying after the given attempt and reports
// whether to go ahead. It says no when the attempts are used up, or when the
// wait would not finish before ctx's deadline: failing now with the real error
// beats sleeping only to time out.
func waitToRetry(ctx context.Context, header http.Header, attempt int) bool {
	if attempt >= maxRetryAttempts {
		return false
	}
	delay := retryDelay(header, attempt)
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
		return false
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
