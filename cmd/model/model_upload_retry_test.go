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
