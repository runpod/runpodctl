package model

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runpod/runpodctl/api"
)

// fastUploadBackoff shortens the retry backoff so retry tests don't sleep for
// the production default. Restores the original on cleanup.
// drainRequestBody consumes a request body in a test handler so the client's
// write completes, reporting (not failing on) a read error from the handler
// goroutine.
func drainRequestBody(t *testing.T, r *http.Request) {
	t.Helper()
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		t.Errorf("drain request body: %v", err)
	}
}

func fastUploadBackoff(t *testing.T) {
	t.Helper()
	orig := modelUploadRetryBackoff
	modelUploadRetryBackoff = time.Millisecond
	t.Cleanup(func() { modelUploadRetryBackoff = orig })
}

// TestCompleteModelUpload_RetriesTransientPartFailure pins that a transient S3
// failure (5xx) on a part PUT is retried rather than failing the whole upload.
// Before this, a single blip aborted a multi-GB upload with no resumption.
func TestCompleteModelUpload_RetriesTransientPartFailure(t *testing.T) {
	fastUploadBackoff(t)
	artifactPath := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(artifactPath, []byte("abcdef"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var putAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			if putAttempts.Add(1) == 1 {
				// first attempt: transient server error
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			drainRequestBody(t, r)
			w.Header().Set("ETag", `"etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   server.URL,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL}},
	}, artifactPath, nil)
	if err != nil {
		t.Fatalf("expected upload to succeed after retrying a transient failure, got %v", err)
	}
	if got := putAttempts.Load(); got < 2 {
		t.Fatalf("expected the part PUT to be retried (>=2 attempts), got %d", got)
	}
}

// TestCompleteModelUpload_DoesNotRetryClientError pins that a 4xx (a real
// client/permission error) fails fast instead of burning retries.
func TestCompleteModelUpload_DoesNotRetryClientError(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(artifactPath, []byte("abcdef"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var putAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putAttempts.Add(1)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   server.URL,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL}},
	}, artifactPath, nil)
	if err == nil {
		t.Fatal("expected a 403 to fail the upload")
	}
	if got := putAttempts.Load(); got != 1 {
		t.Fatalf("expected exactly 1 attempt on a 4xx (no retry), got %d", got)
	}
}

// shortStallTimeout shrinks the no-progress timeout so stall tests finish fast.
// Restores the original on cleanup.
func shortStallTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := modelUploadStallTimeout
	modelUploadStallTimeout = d
	t.Cleanup(func() { modelUploadStallTimeout = orig })
}

// stallingServer starts a server whose handler runs fn and then blocks until
// the test ends, so a client that waits on it without a deadline hangs.
func stallingServer(t *testing.T, fn func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fn(w, r)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	return server
}

// uploadTestDeadline is how long runUploadWithin lets an upload run before
// treating it as hung; far above any stall timeout the tests configure.
const uploadTestDeadline = 10 * time.Second

// runUploadWithin runs a single-part upload of artifactPath and fails the test
// if it does not return within uploadTestDeadline.
func runUploadWithin(t *testing.T, upload *api.ModelRepoUpload, artifactPath string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- completeModelUploadWithProgress(upload, artifactPath, nil) }()
	select {
	case err := <-done:
		return err
	case <-time.After(uploadTestDeadline):
		t.Fatalf("upload still blocked after %s", uploadTestDeadline)
		return nil
	}
}

func writeArtifact(t *testing.T, size int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model.bin")
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatalf("size artifact: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close artifact: %v", err)
	}
	return path
}

// TestCompleteModelUpload_StalledPartWriteTimesOut pins that a server which
// stops consuming a part body mid-transfer cannot hang the upload. The
// ResponseHeaderTimeout only starts after the body is written, so before the
// no-progress timeout this blocked forever.
func TestCompleteModelUpload_StalledPartWriteTimesOut(t *testing.T) {
	fastUploadBackoff(t)
	shortStallTimeout(t, 200*time.Millisecond)
	const partSize = 64 << 20 // large enough to fill socket buffers
	artifactPath := writeArtifact(t, partSize)

	server := stallingServer(t, func(_ http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadFull(r.Body, make([]byte, 8<<10)); err != nil {
			t.Errorf("read first bytes of part: %v", err)
		}
	})

	err := runUploadWithin(t, &api.ModelRepoUpload{
		PartSizeBytes: partSize,
		CompleteURL:   server.URL,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL}},
	}, artifactPath)
	if err == nil {
		t.Fatal("expected a stalled part write to fail the upload")
	}
}

// TestCompleteModelUpload_StalledErrorBodyTimesOut pins that reading an error
// response body is bounded for both retryable (503) and non-retryable (403)
// statuses: headers arrive, then the body stalls short of Content-Length.
func TestCompleteModelUpload_StalledErrorBodyTimesOut(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "retryable 503", status: http.StatusServiceUnavailable},
		{name: "non-retryable 403", status: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fastUploadBackoff(t)
			shortStallTimeout(t, 200*time.Millisecond)
			artifactPath := writeArtifact(t, 6)

			server := stallingServer(t, func(w http.ResponseWriter, r *http.Request) {
				drainRequestBody(t, r)
				w.Header().Set("Content-Length", "1000")
				w.WriteHeader(tt.status)
				if _, err := w.Write([]byte("0123456789")); err != nil {
					t.Errorf("write partial error body: %v", err)
				}
				flusher, ok := w.(http.Flusher)
				if !ok {
					t.Error("response writer does not support flushing")
					return
				}
				flusher.Flush()
			})

			err := runUploadWithin(t, &api.ModelRepoUpload{
				PartSizeBytes: 6,
				CompleteURL:   server.URL,
				Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL}},
			}, artifactPath)
			if err == nil {
				t.Fatalf("expected a %d with a stalled body to fail the upload", tt.status)
			}
		})
	}
}

// TestCompleteModelUpload_SlowResponseHeadersStillSucceed pins that the
// no-progress timeout does not apply while waiting for response headers after
// the body is fully sent (that wait is ResponseHeaderTimeout's job), so a slow
// CompleteMultipartUpload or part ack is not aborted.
func TestCompleteModelUpload_SlowResponseHeadersStillSucceed(t *testing.T) {
	stall := 100 * time.Millisecond
	shortStallTimeout(t, stall)
	artifactPath := writeArtifact(t, 6)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		drainRequestBody(t, r)
		time.Sleep(3 * stall)
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	err := runUploadWithin(t, &api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   server.URL,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL}},
	}, artifactPath)
	if err != nil {
		t.Fatalf("expected slow-but-progressing upload to succeed, got %v", err)
	}
}

// TestCompleteModelUpload_HonorsRetryAfter pins that a throttled part PUT waits
// for the server's Retry-After before retrying instead of the short fixed
// backoff, which would just get throttled again under S3 SlowDown.
func TestCompleteModelUpload_HonorsRetryAfter(t *testing.T) {
	fastUploadBackoff(t)
	artifactPath := writeArtifact(t, 6)

	var putAttempts atomic.Int32
	var firstAt, retryAt time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusOK)
			return
		}
		drainRequestBody(t, r)
		if putAttempts.Add(1) == 1 {
			firstAt = time.Now()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		retryAt = time.Now()
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	err := runUploadWithin(t, &api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   server.URL,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL}},
	}, artifactPath)
	if err != nil {
		t.Fatalf("expected upload to succeed after the throttle, got %v", err)
	}
	if gap := retryAt.Sub(firstAt); gap < 900*time.Millisecond {
		t.Fatalf("expected retry to wait ~1s for Retry-After, waited %s", gap)
	}
}

// TestModelUploadRetryDelay pins Retry-After parsing (delta-seconds and
// HTTP-date), the fallback to exponential backoff, and the upper cap.
func TestModelUploadRetryDelay(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	backoff2 := 2 * modelUploadRetryBackoff // attempt 2 => base << 1
	tests := []struct {
		name       string
		retryAfter string
		want       time.Duration
	}{
		{name: "no header uses backoff", retryAfter: "", want: backoff2},
		{name: "delta seconds", retryAfter: "7", want: 7 * time.Second},
		{name: "zero seconds retries now", retryAfter: "0", want: 0},
		{name: "http date", retryAfter: now.Add(12 * time.Second).Format(http.TimeFormat), want: 12 * time.Second},
		{name: "past http date retries now", retryAfter: now.Add(-time.Minute).Format(http.TimeFormat), want: 0},
		{name: "garbage uses backoff", retryAfter: "soon", want: backoff2},
		{name: "negative uses backoff", retryAfter: "-5", want: backoff2},
		{name: "huge delta is capped", retryAfter: "3600", want: modelUploadMaxRetryAfter},
		{name: "far http date is capped", retryAfter: now.Add(time.Hour).Format(http.TimeFormat), want: modelUploadMaxRetryAfter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modelUploadRetryDelay(2, tt.retryAfter, now); got != tt.want {
				t.Fatalf("modelUploadRetryDelay(2, %q) = %s, want %s", tt.retryAfter, got, tt.want)
			}
		})
	}
}

// TestCompleteModelUpload_TransportErrorIsNotReportedAsStall pins that a plain
// network failure keeps its own error instead of being mislabeled as a
// no-progress stall, which would send someone debugging the wrong timeout.
func TestCompleteModelUpload_TransportErrorIsNotReportedAsStall(t *testing.T) {
	fastUploadBackoff(t)
	artifactPath := writeArtifact(t, 6)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close() // nothing listens here now: connection refused

	err := runUploadWithin(t, &api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   url,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: url}},
	}, artifactPath)
	if err == nil {
		t.Fatal("expected an unreachable server to fail the upload")
	}
	if strings.Contains(err.Error(), "no progress") {
		t.Fatalf("expected a transport error, got it reported as a stall: %v", err)
	}
}

// lateEOFBody yields its payload, then holds back the final EOF read until
// release is closed. The transport makes that read from its write goroutine
// after copying ContentLength bytes, so it can land after Do has returned.
type lateEOFBody struct {
	payload []byte
	release <-chan struct{}
}

func (b *lateEOFBody) Read(p []byte) (int, error) {
	if len(b.payload) > 0 {
		n := copy(p, b.payload)
		b.payload = b.payload[n:]
		return n, nil
	}
	<-b.release
	return 0, io.EOF
}

func (b *lateEOFBody) Close() error { return nil }

// lateEOFResponsePrefix is what the server writes before stalling the
// response body.
const lateEOFResponsePrefix = "0123456789"

// TestGuardedAttempt_LateRequestEOFKeepsResponseGuarded pins that the request
// body hitting EOF after the response headers arrived does not disarm the
// stall guard for the response body. Before the fix the late EOF stopped the
// timer, so a response body that stalled mid-stream blocked forever.
func TestGuardedAttempt_LateRequestEOFKeepsResponseGuarded(t *testing.T) {
	shortStallTimeout(t, 200*time.Millisecond)
	release := make(chan struct{})
	server := stallingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.CopyN(io.Discard, r.Body, 6); err != nil {
			t.Errorf("read request body: %v", err)
		}
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		if _, err := io.WriteString(w, lateEOFResponsePrefix); err != nil {
			t.Errorf("write partial body: %v", err)
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("response writer does not support flushing")
			return
		}
		flusher.Flush()
	})

	newReq := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL, &lateEOFBody{payload: []byte("abcdef"), release: release})
		if err != nil {
			return nil, err
		}
		req.ContentLength = 6
		return req, nil
	}

	done := make(chan error, 1)
	go func() {
		resp, err := doGuardedModelUploadAttempt(newReq)
		if err != nil {
			done <- err
			return
		}
		// consume every byte the server sent before releasing the request EOF,
		// so no later response progress can re-arm a timer the EOF wrongly
		// stopped.
		if _, err := io.ReadFull(resp.Body, make([]byte, len(lateEOFResponsePrefix))); err != nil {
			done <- err
			return
		}
		close(release)
		_, err = io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the stalled response body to fail the read")
		}
	case <-time.After(uploadTestDeadline):
		t.Fatalf("response body read still blocked after %s", uploadTestDeadline)
	}
}
