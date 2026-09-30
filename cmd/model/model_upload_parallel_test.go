package model

import (
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
	if err := os.WriteFile(artifactPath, make([]byte, partCount), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var inFlight, maxInFlight int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			cur := atomic.AddInt32(&inFlight, 1)
			defer atomic.AddInt32(&inFlight, -1)
			for {
				obs := atomic.LoadInt32(&maxInFlight)
				if cur <= obs || atomic.CompareAndSwapInt32(&maxInFlight, obs, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond) // hold open so calls actually overlap
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

	if got := atomic.LoadInt32(&maxInFlight); got <= 1 {
		t.Fatalf("expected parts to upload concurrently (max in flight > 1), got %d", got)
	}
	if got := progress.total(); got != partCount {
		t.Fatalf("expected progress to total %d bytes, got %d", partCount, got)
	}
}

// TestCompleteModelUpload_PreservesPartOrderUnderConcurrency pins that ETags are
// assembled in ascending part-number order regardless of which PUT finishes
// first — S3 CompleteMultipartUpload requires ordered parts.
func TestCompleteModelUpload_PreservesPartOrderUnderConcurrency(t *testing.T) {
	const partCount = 6
	artifactPath := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(artifactPath, make([]byte, partCount), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var mu sync.Mutex
	var completionBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			p, _ := strconv.Atoi(r.URL.Query().Get("p"))
			// finish higher part numbers first to scramble completion order
			time.Sleep(time.Duration(partCount-p) * 5 * time.Millisecond)
			_, _ = io.ReadAll(r.Body)
			w.Header().Set("ETag", `"etag-`+r.URL.Query().Get("p")+`"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			b, _ := io.ReadAll(r.Body)
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
