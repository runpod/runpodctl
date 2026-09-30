package api

import (
	"net/http"
	"strconv"
	"time"
)

// maxRetryAttempts bounds total tries (initial + retries) for a transient
// failure.
const maxRetryAttempts = 3

// retryBackoff is the base delay for exponential backoff between retries. It is
// a var so tests can shorten it.
var retryBackoff = 500 * time.Millisecond

// idempotentMethod reports whether re-issuing a request with this method is safe
// even if the previous attempt may have reached the server. Only pure reads
// qualify: retrying a POST/PATCH could create or mutate a resource twice (e.g. a
// second billed pod), so those are never retried on a 5xx or network error.
func idempotentMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// retryableStatus reports whether an HTTP status warrants a retry for the given
// method. A 429 is safe to retry for any method: it means the request was
// rejected (rate limited) before processing. A 5xx is retried only for
// idempotent methods.
func retryableStatus(method string, statusCode int) bool {
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	if statusCode >= 500 {
		return idempotentMethod(method)
	}
	return false
}

// retryDelay is how long to wait before the given (1-based) attempt's retry.
// It honors a Retry-After header (seconds) when present, otherwise uses
// exponential backoff.
func retryDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if v := resp.Header.Get("Retry-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
				return time.Duration(secs) * time.Second
			}
		}
	}
	return retryBackoff * time.Duration(1<<(attempt-1))
}
