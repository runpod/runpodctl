package api

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// writeOversized sends status and then MaxResponseBytes+2 bytes, which
// ReadBounded rejects.
func writeOversized(t *testing.T, w http.ResponseWriter, status int) {
	t.Helper()
	w.Header().Set("Retry-After", "0")
	w.WriteHeader(status)
	chunk := make([]byte, 1<<20)
	for range (MaxResponseBytes >> 20) + 1 {
		if _, err := w.Write(chunk); err != nil {
			return // the client stops reading once over the limit
		}
	}
}

// writeTruncated sends status, promises more bytes than it sends and drops the
// connection. Content-Length must be set before WriteHeader or it is ignored.
func writeTruncated(t *testing.T, w http.ResponseWriter, status int) {
	t.Helper()
	w.Header().Set("Retry-After", "0")
	w.Header().Set("Content-Length", "100000")
	w.WriteHeader(status)
	if _, err := w.Write([]byte(`{"error":"cut`)); err != nil {
		t.Errorf("write: %v", err)
	}
}

// TestRejectedStatusKeepsStatusWhenBodyUnreadable: a received error status whose
// body is oversized or cut short must still surface as an APIError carrying the
// status and its stable code, after the usual retries.
func TestRejectedStatusKeepsStatusWhenBodyUnreadable(t *testing.T) {
	bodies := map[string]func(*testing.T, http.ResponseWriter, int){
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
						write(t, w, status)
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

// a received error status whose body dies with a connection reset keeps its
// status and code, but the (redacted) network failure stays reachable through
// errors.As, so it still classifies as the transient network failure it is.
func TestUnreadableBodyOnRejectedStatusKeepsNetworkCause(t *testing.T) {
	readErr := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("read: connection reset by peer")}
	_, err := ReadBodyForStatus(http.StatusBadGateway, failingReader{err: readErr})
	if err == nil {
		t.Fatal("expected an error")
	}
	apiErr, isAPIError := errors.AsType[*APIError](err)
	if !isAPIError {
		t.Fatalf("expected an *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != http.StatusBadGateway || apiErr.ErrorCode() != "server_error" {
		t.Fatalf("the received status must win: status=%d code=%q", apiErr.Status, apiErr.ErrorCode())
	}
	opErr, isNetError := errors.AsType[*net.OpError](err)
	if !isNetError {
		t.Fatalf("the network cause must stay reachable through Unwrap: %v", err)
	}
	if opErr.Op != "read" {
		t.Fatalf("expected the read net.OpError, got %v", opErr)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("the message should name the read failure, got %v", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// a body that cannot be read on a 2xx is still the read error, not an APIError.
func TestSuccessStatusWithUnreadableBodyStaysReadError(t *testing.T) {
	withFastRetry(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTruncated(t, w, http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	_, err := client.requestURL(http.MethodGet, server.URL+"/x", nil)
	if err == nil {
		t.Fatal("expected a read error")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) { //nolint:modernize // errors.As, not AsType: nix errcheck flags AsType
		t.Fatalf("a 2xx read failure must not be an APIError: %v", err)
	}
}
