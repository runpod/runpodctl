package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func withFastRetry(t *testing.T) {
	t.Helper()
	orig := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = orig })
}

func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_API_URL", url)
	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.baseURL = url
	return c
}

// TestGetRetriesTransient5xx: an idempotent GET must be retried through a
// transient 5xx and ultimately succeed.
func TestGetRetriesTransient5xx(t *testing.T) {
	withFastRetry(t)
	var n int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	if _, err := client.Get("/x", nil); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if got := atomic.LoadInt32(&n); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

// TestPostDoesNotRetry5xx: a POST is not idempotent (e.g. pod create), so a 5xx
// must NOT be retried — retrying could create the resource twice (double spend).
func TestPostDoesNotRetry5xx(t *testing.T) {
	withFastRetry(t)
	var n int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	if _, err := client.Post("/x", map[string]string{"a": "b"}); err == nil {
		t.Fatal("expected error on 503")
	}
	if got := atomic.LoadInt32(&n); got != 1 {
		t.Fatalf("expected exactly 1 attempt for POST on 5xx (no retry), got %d", got)
	}
}

// TestPostRetries429: a 429 means the request was rejected before processing, so
// retrying is safe even for a non-idempotent POST. Retry-After is honored.
func TestPostRetries429(t *testing.T) {
	withFastRetry(t)
	var n int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	if _, err := client.Post("/x", map[string]string{"a": "b"}); err != nil {
		t.Fatalf("expected success after 429 retry, got %v", err)
	}
	if got := atomic.LoadInt32(&n); got != 2 {
		t.Fatalf("expected 2 attempts after 429, got %d", got)
	}
}
