package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

// writeOversized sends MaxResponseBytes+2 bytes, which ReadBounded rejects.
func writeOversized(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	chunk := make([]byte, 1<<20)
	for i := 0; i < (MaxResponseBytes>>20)+1; i++ {
		if _, err := w.Write(chunk); err != nil {
			return // the client stops reading once over the limit
		}
	}
}

// writeTruncated promises more bytes than it sends and drops the connection.
func writeTruncated(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Length", "100000")
	if _, err := w.Write([]byte(`{"error":"cut`)); err != nil {
		t.Errorf("write: %v", err)
	}
}

// TestRejectedStatusKeepsStatusWhenBodyUnreadable: a received error status whose
// body is oversized or cut short must still surface as an APIError carrying the
// status and its stable code, after the usual retries.
func TestRejectedStatusKeepsStatusWhenBodyUnreadable(t *testing.T) {
	bodies := map[string]func(*testing.T, http.ResponseWriter){
		"oversized": writeOversized,
		"truncated": writeTruncated,
	}
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		wantCode := map[int]string{http.StatusTooManyRequests: "rate_limited", http.StatusServiceUnavailable: "server_error"}[status]
		for bodyName, write := range bodies {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				t.Run(strconv.Itoa(status)+"/"+bodyName+"/"+method, func(t *testing.T) {
					withFastRetry(t)
					var n atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						n.Add(1)
						w.Header().Set("Retry-After", "0")
						w.WriteHeader(status)
						write(t, w)
					}))
					t.Cleanup(server.Close)

					client := newTestClient(t, server.URL)
					_, err := client.requestURL(method, server.URL+"/x", nil)
					var apiErr *APIError
					if !errors.As(err, &apiErr) {
						t.Fatalf("expected an *APIError, got %T: %v", err, err)
					}
					if apiErr.Status != status {
						t.Fatalf("status = %d, want %d", apiErr.Status, status)
					}
					if got := apiErr.ErrorCode(); got != wantCode {
						t.Fatalf("code = %q, want %q", got, wantCode)
					}
					wantAttempts := int32(1) // a write is never retried
					if method == http.MethodGet {
						wantAttempts = maxRetryAttempts
					}
					if got := n.Load(); got != wantAttempts {
						t.Fatalf("attempts = %d, want %d", got, wantAttempts)
					}
				})
			}
		}
	}
}

// a body that cannot be read on a 2xx is still the read error, not an APIError.
func TestSuccessStatusWithUnreadableBodyStaysReadError(t *testing.T) {
	withFastRetry(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTruncated(t, w)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	_, err := client.requestURL(http.MethodGet, server.URL+"/x", nil)
	if err == nil {
		t.Fatal("expected a read error")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("a 2xx read failure must not be an APIError: %v", err)
	}
}
