package model

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runpod/runpodctl/api"
)

// TestCompleteModelUpload_UploadsPartsConcurrently pins that the parts of a
// single file upload in parallel (bounded), not strictly sequentially. A model
// repo is commonly one large multi-part file, so sequential part PUTs meant zero
// upload parallelism in the common case.
func TestCompleteModelUpload_UploadsPartsConcurrently(t *testing.T) {
	const partCount = 8
	artifactPath := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(artifactPath, make([]byte, partCount), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var inFlight, maxInFlight atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			cur := inFlight.Add(1)
			defer inFlight.Add(-1)
			for {
				obs := maxInFlight.Load()
				if cur <= obs || maxInFlight.CompareAndSwap(obs, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond) // hold open so calls actually overlap
			drainRequestBody(t, r)
			w.Header().Set("ETag", `"etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	parts := make([]*api.ModelRepoUploadPart, partCount)
	for i := range parts {
		parts[i] = &api.ModelRepoUploadPart{PartNumber: i + 1, URL: server.URL}
	}

	progress := &recordingModelUploadProgress{}
	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 1,
		CompleteURL:   server.URL,
		Parts:         parts,
	}, artifactPath, progress)
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}

	got := maxInFlight.Load()
	if got <= 1 {
		t.Fatalf("expected parts to upload concurrently (max in flight > 1), got %d", got)
	}
	if got > modelPartUploadConcurrency {
		t.Fatalf("expected at most %d part uploads in flight, observed %d", modelPartUploadConcurrency, got)
	}
	if got := progress.total(); got != partCount {
		t.Fatalf("expected progress to total %d bytes, got %d", partCount, got)
	}
}

// TestCompleteModelUpload_PreservesPartOrderUnderConcurrency pins that ETags are
// assembled in ascending part-number order regardless of which PUT finishes
// first. S3 CompleteMultipartUpload requires ordered parts.
func TestCompleteModelUpload_PreservesPartOrderUnderConcurrency(t *testing.T) {
	const partCount = 6
	artifactPath := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(artifactPath, make([]byte, partCount), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var mu sync.Mutex
	var completionBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			p, err := strconv.Atoi(r.URL.Query().Get("p"))
			if err != nil {
				t.Errorf("part query: %v", err)
			}
			// finish higher part numbers first to scramble completion order
			time.Sleep(time.Duration(partCount-p) * 5 * time.Millisecond)
			drainRequestBody(t, r)
			w.Header().Set("ETag", `"etag-`+r.URL.Query().Get("p")+`"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read completion body: %v", err)
			}
			mu.Lock()
			completionBody = string(b)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(server.Close)

	parts := make([]*api.ModelRepoUploadPart, partCount)
	for i := range parts {
		parts[i] = &api.ModelRepoUploadPart{PartNumber: i + 1, URL: server.URL + "?p=" + strconv.Itoa(i+1)}
	}

	if err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 1,
		CompleteURL:   server.URL,
		Parts:         parts,
	}, artifactPath, nil); err != nil {
		t.Fatalf("complete upload: %v", err)
	}

	mu.Lock()
	body := completionBody
	mu.Unlock()

	// each part's ETag ("etag-N") must appear in ascending part-number order.
	last := -1
	for i := 1; i <= partCount; i++ {
		idx := strings.Index(body, "etag-"+strconv.Itoa(i))
		if idx < 0 {
			t.Fatalf("completion body missing part %d: %s", i, body)
		}
		if idx < last {
			t.Fatalf("parts out of order in completion body: %s", body)
		}
		last = idx
	}
}

// TestCompleteModelUpload_FailedPartStopsRemainingParts pins that one failed
// part ends the upload instead of sending the rest of the file. With an expired
// presigned URL every part 403s, so without cancellation a multi-GB file was
// uploaded in full before the error surfaced.
func TestCompleteModelUpload_FailedPartStopsRemainingParts(t *testing.T) {
	const partCount = 40
	artifactPath := writeArtifact(t, partCount)

	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected %s: completion must not run after a failed part", r.Method)
			w.WriteHeader(http.StatusOK)
			return
		}
		puts.Add(1)
		// no drainRequestBody: once part 1 fails the client cancels the parts
		// still in flight, so their bodies can end in a closed connection or
		// an unexpected EOF. That cancellation is what this test pins.
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Query().Get("p") == "1" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		// hold the other parts so they are still in flight when part 1 fails.
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	parts := make([]*api.ModelRepoUploadPart, partCount)
	for i := range parts {
		parts[i] = &api.ModelRepoUploadPart{PartNumber: i + 1, URL: server.URL + "?p=" + strconv.Itoa(i+1)}
	}

	err := runUploadWithin(t, &api.ModelRepoUpload{
		PartSizeBytes: 1,
		CompleteURL:   server.URL,
		Parts:         parts,
	}, artifactPath)
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected the part 1 403 to fail the upload, got %v", err)
	}
	if got := puts.Load(); got > modelPartUploadConcurrency {
		t.Fatalf("expected at most %d part PUTs (those already in flight), got %d of %d", modelPartUploadConcurrency, got, partCount)
	}
}

// TestDoModelUploadRequest_StopsWhenCanceled pins that a canceled upload sends
// no further attempts, so a part waiting to retry does not resend its body
// after another part has already failed the upload.
func TestDoModelUploadRequest_StopsWhenCanceled(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		drainRequestBody(t, r)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp, err := doModelUploadRequest(ctx, "upload part 1", func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPut, server.URL, strings.NewReader("abc"))
	})
	if resp != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
		t.Fatalf("expected no response after cancel, got status %d", resp.StatusCode)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a context.Canceled error, got %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("expected no requests after cancel, got %d", got)
	}
}

// TestRunBounded_StopsLaunchingAfterFirstError pins that runBounded cancels the
// shared context on the first failure and starts no further work, and that the
// real failure (not a cancellation it caused) is what it returns.
func TestRunBounded_StopsLaunchingAfterFirstError(t *testing.T) {
	const n = 20
	boom := errors.New("boom")
	var started atomic.Int32
	err := runBounded(n, 2, func(ctx context.Context, i int) error {
		started.Add(1)
		if i == 1 {
			return boom
		}
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected the boom failure, got %v", err)
	}
	if got := started.Load(); got > 3 {
		t.Fatalf("expected no work launched after the failure (<= 3 started), got %d", got)
	}
}
