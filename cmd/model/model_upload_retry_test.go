package model

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runpod/runpodctl/api"
)

// fastUploadBackoff shortens the retry backoff so retry tests don't sleep for
// the production default. Restores the original on cleanup.
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
	if err := os.WriteFile(artifactPath, []byte("abcdef"), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var putAttempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			if atomic.AddInt32(&putAttempts, 1) == 1 {
				// first attempt: transient server error
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = io.ReadAll(r.Body)
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
	if got := atomic.LoadInt32(&putAttempts); got < 2 {
		t.Fatalf("expected the part PUT to be retried (>=2 attempts), got %d", got)
	}
}

// TestCompleteModelUpload_DoesNotRetryClientError pins that a 4xx (a real
// client/permission error) fails fast instead of burning retries.
func TestCompleteModelUpload_DoesNotRetryClientError(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(artifactPath, []byte("abcdef"), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var putAttempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			atomic.AddInt32(&putAttempts, 1)
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
	if got := atomic.LoadInt32(&putAttempts); got != 1 {
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

// runUploadWithin runs a single-part upload of artifactPath and fails the test
// if it does not return within limit.
func runUploadWithin(t *testing.T, limit time.Duration, upload *api.ModelRepoUpload, artifactPath string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- completeModelUploadWithProgress(upload, artifactPath, nil) }()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		t.Fatalf("upload still blocked after %s", limit)
		return nil
	}
}

func writeArtifact(t *testing.T, size int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model.bin")
	f, err := os.Create(path)
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

	server := stallingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadFull(r.Body, make([]byte, 8<<10))
	})

	err := runUploadWithin(t, 10*time.Second, &api.ModelRepoUpload{
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
				_, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Length", "1000")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("0123456789"))
				w.(http.Flusher).Flush()
			})

			err := runUploadWithin(t, 10*time.Second, &api.ModelRepoUpload{
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
		_, _ = io.ReadAll(r.Body)
		time.Sleep(3 * stall)
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	err := runUploadWithin(t, 10*time.Second, &api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   server.URL,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL}},
	}, artifactPath)
	if err != nil {
		t.Fatalf("expected slow-but-progressing upload to succeed, got %v", err)
	}
}
