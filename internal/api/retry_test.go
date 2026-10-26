package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func testResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestGetRequestConstructionErrorDoesNotRetry(t *testing.T) {
	withRetryBackoff(t, time.Second)
	client := newTestClient(t, "http://bad url")
	start := time.Now()
	_, err := client.Get("/x", nil)
	if !errors.Is(err, errRequestConstruction) {
		t.Fatalf("expected request construction error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("request construction took %v; it appears to have waited to retry", elapsed)
	}
}

func TestRetryWaitStopsOnSignal(t *testing.T) {
	if os.Getenv("TEST_RETRY_SIGNAL_CHILD") == "1" {
		withRetryBackoff(t, 5*time.Second)
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		}()
		if waitToRetry(context.Background(), nil, 1) {
			t.Fatal("retry continued after signal")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRetryWaitStopsOnSignal$")
	cmd.Env = append(os.Environ(), "TEST_RETRY_SIGNAL_CHILD=1")
	start := time.Now()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("signal child failed: %v: %s", err, out)
	}
	// the child's backoff is 5s; this bound includes process start (slow under
	// -race), so it only has to show the wait was cut short.
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("signal took %v to stop retry wait", elapsed)
	}
}

func TestPermanentServerErrorsAreNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusNotImplemented, http.StatusHTTPVersionNotSupported} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var attempts atomic.Int32
			client := newTestClient(t, "https://example.invalid")
			client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				attempts.Add(1)
				return testResponse(status, ""), nil
			})
			if _, err := client.Get("/x", nil); err == nil {
				t.Fatal("expected server error")
			}
			if got := attempts.Load(); got != 1 {
				t.Fatalf("expected 1 attempt, got %d", got)
			}
		})
	}
	if !retryableStatus(http.MethodGet, http.StatusServiceUnavailable) {
		t.Fatal("get 503 should still be retried")
	}
	if retryableStatus(http.MethodGet, 600) {
		t.Fatal("status 600 should not be retried")
	}
}

func TestWorkerListRetriesTransientGet(t *testing.T) {
	withFastRetry(t)
	var attempts atomic.Int32
	client := newTestClient(t, "https://example.invalid")
	client.httpClient.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			return testResponse(http.StatusServiceUnavailable, ""), nil
		}
		return testResponse(http.StatusOK, `{"workers":[{"id":"worker-1"}]}`), nil
	})

	v2 := &V2Client{baseURL: client.baseURL, apiKey: client.apiKey, httpClient: client.httpClient, userAgent: client.userAgent}
	workers, err := v2.ListEndpointWorkers(context.Background(), "endpoint-1")
	if err != nil || len(workers.Workers) != 1 {
		t.Fatalf("worker listing after retry: %v, %v", workers, err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func withFastRetry(t *testing.T) {
	t.Helper()
	withRetryBackoff(t, time.Millisecond)
}

func withRetryBackoff(t *testing.T, d time.Duration) {
	t.Helper()
	orig := retryBackoff
	retryBackoff = d
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

func writeBody(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := w.Write([]byte(body)); err != nil {
		t.Errorf("write response: %v", err)
	}
}

// TestGetRetriesTransient5xx: an idempotent GET must be retried through a
// transient 5xx and ultimately succeed.
func TestGetRetriesTransient5xx(t *testing.T) {
	withFastRetry(t)
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		writeBody(t, w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	if _, err := client.Get("/x", nil); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if got := n.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

// TestGetRetries429: a rate-limited read is retried and Retry-After is honored.
func TestGetRetries429(t *testing.T) {
	withFastRetry(t)
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeBody(t, w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	if _, err := client.Get("/x", nil); err != nil {
		t.Fatalf("expected success after 429 retry, got %v", err)
	}
	if got := n.Load(); got != 2 {
		t.Fatalf("expected 2 attempts after 429, got %d", got)
	}
}

// TestWritesAreNeverRetried: a write is sent exactly once whatever comes back.
// a 429 does not prove the write did nothing: rest v2 pod create runs the
// create and then refetches the pod, and a 429 on that refetch is passed
// through even though the pod exists, so a retried POST bills a second pod.
func TestWritesAreNeverRetried(t *testing.T) {
	body := map[string]string{"a": "b"}
	tests := []struct {
		name   string
		status int
		call   func(c *Client) error
	}{
		{"post 429", http.StatusTooManyRequests, func(c *Client) error { _, err := c.Post("/x", body); return err }},
		{"post 503", http.StatusServiceUnavailable, func(c *Client) error { _, err := c.Post("/x", body); return err }},
		{"patch 429", http.StatusTooManyRequests, func(c *Client) error { _, err := c.Patch("/x", body); return err }},
		{"delete 429", http.StatusTooManyRequests, func(c *Client) error { _, err := c.Delete("/x"); return err }},
		{"v2 post 429", http.StatusTooManyRequests, func(c *Client) error { _, err := c.PostV2("/pods", body); return err }},
		{"v2 put 429", http.StatusTooManyRequests, func(c *Client) error { _, err := c.PutV2("/x", body); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFastRetry(t)
			var n atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(server.Close)

			client := newTestClient(t, server.URL)
			client.v2BaseURL = server.URL
			if err := tt.call(client); err == nil {
				t.Fatalf("expected error on %d", tt.status)
			}
			if got := n.Load(); got != 1 {
				t.Fatalf("expected exactly 1 attempt, got %d", got)
			}
		})
	}
}

// TestRetryAfterIsCapped: a huge Retry-After must not park the cli for minutes.
func TestRetryAfterIsCapped(t *testing.T) {
	header := http.Header{"Retry-After": []string{"3600"}}
	if got := retryDelay(header, 1); got != maxRetryAfter {
		t.Fatalf("retryDelay = %v, want cap %v", got, maxRetryAfter)
	}
}

// TestRetriesShareOneDeadline: the configured timeout bounds the whole logical
// call, attempts and backoff sleeps together, not each attempt separately.
func TestRetriesShareOneDeadline(t *testing.T) {
	const timeout = 300 * time.Millisecond
	withRetryBackoff(t, 200*time.Millisecond)
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	client.httpClient.Timeout = timeout
	start := time.Now()
	if _, err := client.Get("/x", nil); err == nil {
		t.Fatal("expected error on persistent 503")
	}
	// 200ms + 400ms of backoff without a shared deadline; with one, the second
	// sleep would overrun it, so the call gives up after the second attempt.
	if elapsed := time.Since(start); elapsed > timeout+100*time.Millisecond {
		t.Fatalf("call took %v, want it bounded by the %v timeout", elapsed, timeout)
	}
	if got := n.Load(); got != 2 {
		t.Fatalf("expected 2 attempts inside the deadline, got %d", got)
	}
}

// TestRetryAfterBeyondDeadlineGivesUp: a Retry-After longer than the time left
// fails now with the 429 instead of sleeping past the deadline.
func TestRetryAfterBeyondDeadlineGivesUp(t *testing.T) {
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	client.httpClient.Timeout = time.Second
	start := time.Now()
	_, err := client.Get("/x", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		t.Fatalf("expected the 429 APIError, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("call took %v, want an immediate give-up", elapsed)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("expected 1 attempt, got %d", got)
	}
}

// TestGet429WithTruncatedBodyIsRetried: a known 429 follows the retry policy
// even when its body can't be read.
func TestGet429WithTruncatedBodyIsRetried(t *testing.T) {
	withFastRetry(t)
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			// promise more than is sent so the client read ends in unexpected EOF
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(http.StatusTooManyRequests)
			writeBody(t, w, `{"err`)
			return
		}
		writeBody(t, w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	if _, err := client.Get("/x", nil); err != nil {
		t.Fatalf("expected success after truncated 429, got %v", err)
	}
	if got := n.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

// TestRedirectIsNotRetried: a 3xx is reported once, never retried or followed.
func TestRedirectIsNotRetried(t *testing.T) {
	withFastRetry(t)
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	if _, err := client.Get("/x", nil); err == nil {
		t.Fatal("expected redirect error")
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("expected 1 attempt, got %d", got)
	}
}
