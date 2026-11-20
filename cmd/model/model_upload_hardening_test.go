package model

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/runpod/runpodctl/api"
)

func writeUploadArtifact(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return path
}

func TestValidateModelUploadURL(t *testing.T) {
	const (
		httpsAPI    = "https://api.runpod.io/graphql"
		loopbackAPI = "http://localhost:4000/graphql"
	)
	cases := []struct {
		name   string
		url    string
		optIn  bool
		apiURL string
		ok     bool
	}{
		{"https, no opt-in", "https://bucket.s3.amazonaws.com/key?X-Amz-Signature=abc", false, httpsAPI, true},
		{"https, default api url", "https://bucket.s3.amazonaws.com/key", false, "", true},
		{"http loopback upload url, https api url", "http://127.0.0.1:9000/key", false, httpsAPI, false},
		{"http localhost upload url, default api url", "http://localhost:9000/key", false, "", false},
		{"http loopback upload url, loopback api url (dev stack)", "http://127.0.0.1:9000/key", false, loopbackAPI, true},
		{"http minio, loopback api url (dev stack)", "http://minio:9000/key", false, loopbackAPI, true},
		{"http minio with opt-in", "http://minio:9000/key", true, httpsAPI, true},
		{"http lan ip with opt-in", "http://192.168.1.10:9000/key", true, "", true},
		{"http minio without opt-in", "http://minio:9000/key", false, httpsAPI, false},
		{"http remote without opt-in", "http://bucket.s3.amazonaws.com/key?X-Amz-Signature=secretsig", false, httpsAPI, false},
		{"ftp even with opt-in", "ftp://example.com/key", true, loopbackAPI, false},
		{"file scheme", "file:///etc/passwd", true, loopbackAPI, false},
		{"scheme-relative", "//example.com/key", true, loopbackAPI, false},
		{"relative path", "/relative/path", true, loopbackAPI, false},
		{"empty", "", true, loopbackAPI, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setUploadHTTPPolicy(t, tc.optIn, tc.apiURL)
			err := validateModelUploadURL("part 1", tc.url)
			if tc.ok {
				if err != nil {
					t.Fatalf("expected accepted, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected refused")
			}
			if strings.Contains(err.Error(), "secretsig") {
				t.Errorf("error leaks the url query: %v", err)
			}
		})
	}
}

// TestValidateModelUploadURL_RefusalNamesTheOptIn pins that a refused http url
// tells the user how to allow it.
func TestValidateModelUploadURL_RefusalNamesTheOptIn(t *testing.T) {
	setUploadHTTPPolicy(t, false, "https://api.runpod.io/graphql")
	err := validateModelUploadURL("part 1", "http://minio:9000/key")
	if err == nil {
		t.Fatal("expected refused")
	}
	for _, want := range []string{"--insecure", "RUNPOD_ALLOW_INSECURE_API=1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// TestCompleteModelUpload_RefusesHTTPUploadURLWithoutOptIn pins that a plain-http
// part or completion url is refused before any request, including a loopback one
// handed out by the api response while the api url is https.
func TestCompleteModelUpload_RefusesHTTPUploadURLWithoutOptIn(t *testing.T) {
	setUploadHTTPPolicy(t, false, "https://api.runpod.io/graphql")
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer server.Close()

	artifact := writeUploadArtifact(t, "abcdef")
	for name, upload := range map[string]*api.ModelRepoUpload{
		"part": {
			PartSizeBytes: 6,
			CompleteURL:   "https://s3.example.com/complete",
			Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL + "/part?sig=secretsig"}},
		},
		"complete": {
			PartSizeBytes: 6,
			CompleteURL:   server.URL + "/complete?sig=secretsig",
			Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: "https://s3.example.com/part"}},
		},
	} {
		err := completeModelUploadWithProgress(upload, artifact, nil)
		if err == nil || !strings.Contains(err.Error(), "must be https") {
			t.Errorf("%s: expected https refusal, got %v", name, err)
		} else if strings.Contains(err.Error(), "secretsig") {
			t.Errorf("%s: error leaks signature: %v", name, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("the loopback server received %d request(s); expected none", hits.Load())
	}
}

// TestCompleteModelUpload_PartRedirectIsFailure pins that a 301/302/303/307 on a
// part PUT is neither followed nor reported as success.
func TestCompleteModelUpload_PartRedirectIsFailure(t *testing.T) {
	fastUploadBackoff(t)
	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect} {
		var targetHits atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			targetHits.Add(1)
			w.Header().Set("ETag", `"etag"`)
		}))
		redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			drainRequestBody(t, r)
			http.Redirect(w, r, target.URL, code)
		}))

		err := completeModelUploadWithProgress(&api.ModelRepoUpload{
			PartSizeBytes: 6,
			CompleteURL:   target.URL,
			Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: redirector.URL}},
		}, writeUploadArtifact(t, "abcdef"), nil)
		if err == nil || !strings.Contains(err.Error(), "redirect not followed") {
			t.Errorf("status %d: expected redirect failure, got %v", code, err)
		}
		if got := targetHits.Load(); got != 0 {
			t.Errorf("status %d: redirect target was contacted %d time(s)", code, got)
		}
		redirector.Close()
		target.Close()
	}
}

func TestCompleteModelUpload_CompletionRedirectIsFailure(t *testing.T) {
	fastUploadBackoff(t)
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetHits.Add(1)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		drainRequestBody(t, r)
		if r.Method == http.MethodPut {
			w.Header().Set("ETag", `"etag"`)
			return
		}
		http.Redirect(w, r, target.URL, http.StatusSeeOther)
	}))
	defer server.Close()

	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   server.URL,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL}},
	}, writeUploadArtifact(t, "abcdef"), nil)
	if err == nil || !strings.Contains(err.Error(), "completion request failed") {
		t.Fatalf("expected completion redirect failure, got %v", err)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("redirect target was contacted")
	}
}

// TestCompleteModelUpload_NullPartIsError pins that `parts: [null]` is an error,
// not a nil dereference.
func TestCompleteModelUpload_NullPartIsError(t *testing.T) {
	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   "https://s3.example.com/complete",
		Parts:         []*api.ModelRepoUploadPart{nil},
	}, writeUploadArtifact(t, "abcdef"), nil)
	if err == nil || !strings.Contains(err.Error(), "null") {
		t.Fatalf("expected null-part error, got %v", err)
	}
}

// TestCompleteModelUpload_ShrunkFileIsError pins that a file truncated after the
// upload started fails instead of uploading short parts and completing. The
// first part's handler truncates the file before answering, so every part
// started afterwards (there are more parts than part workers) reads past the
// new end of file.
func TestCompleteModelUpload_ShrunkFileIsError(t *testing.T) {
	fastUploadBackoff(t)
	partCount := modelPartUploadConcurrency + 2
	artifact := writeUploadArtifact(t, strings.Repeat("x", partCount))

	var truncated sync.Once
	var completes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			completes.Add(1)
			return
		}
		// parts started after the truncation abort mid-body, so a read error
		// here is expected and not a failure of the handler.
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		truncated.Do(func() {
			if err := os.Truncate(artifact, 0); err != nil {
				t.Errorf("truncate: %v", err)
			}
		})
		w.Header().Set("ETag", `"etag"`)
	}))
	defer server.Close()

	parts := make([]*api.ModelRepoUploadPart, partCount)
	for i := range parts {
		parts[i] = &api.ModelRepoUploadPart{PartNumber: i + 1, URL: server.URL}
	}
	err := completeModelUploadWithContext(context.Background(), &api.ModelRepoUpload{
		PartSizeBytes: 1,
		CompleteURL:   server.URL,
		Parts:         parts,
	}, artifact, nil, nil)
	if !errors.Is(err, errUploadSourceShrank) {
		t.Fatalf("expected errUploadSourceShrank, got %v", err)
	}
	if completes.Load() != 0 {
		t.Fatalf("completion must not be sent for a short upload")
	}
}

// TestCompleteModelUpload_SizeChangedSinceScanIsRefused pins that a single-part
// file whose size differs from the scan (shrunk, emptied or grown) is refused
// before any request, instead of being uploaded with chunk sizes derived from
// the new size.
func TestCompleteModelUpload_SizeChangedSinceScanIsRefused(t *testing.T) {
	for name, newContent := range map[string]string{
		"shrunk":  "abc",
		"emptied": "",
		"grown":   "abcdefghijkl",
	} {
		t.Run(name, func(t *testing.T) {
			artifact := writeUploadArtifact(t, "abcdef")
			scanned, err := os.Lstat(artifact)
			if err != nil {
				t.Fatal(err)
			}
			if writeErr := os.WriteFile(artifact, []byte(newContent), 0o600); writeErr != nil {
				t.Fatal(writeErr)
			}

			server, hits := newUnreachableUploadServer(t)
			err = completeModelUploadWithContext(context.Background(), &api.ModelRepoUpload{
				PartSizeBytes: 6,
				CompleteURL:   server.URL,
				Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL}},
			}, artifact, scanned, nil)
			if !errors.Is(err, errUploadSourceSizeChanged) || !strings.Contains(err.Error(), "size changed since") {
				t.Fatalf("expected the size change to be refused, got %v", err)
			}
			if hits() != 0 {
				t.Fatalf("nothing may be sent, server saw %d request(s)", hits())
			}
		})
	}
}

func TestExactReader_ShortSourceFails(t *testing.T) {
	r := &exactReader{r: strings.NewReader("abc"), want: 5}
	buf := make([]byte, 16)
	var err error
	for err == nil {
		_, err = r.Read(buf)
	}
	if !errors.Is(err, errUploadSourceShrank) {
		t.Fatalf("expected errUploadSourceShrank, got %v", err)
	}
}

func TestExactReader_FullSourceReachesEOF(t *testing.T) {
	r := &exactReader{r: strings.NewReader("abcde"), want: 5}
	buf := make([]byte, 16)
	var err error
	for err == nil {
		_, err = r.Read(buf)
	}
	if err.Error() != "EOF" {
		t.Fatalf("expected EOF, got %v", err)
	}
}

// TestUploadModelFiles_FailedCompletionStatusIsError pins that a completion the
// server reports as success:true but status FAILED fails the command.
func TestUploadModelFiles_FailedCompletionStatusIsError(t *testing.T) {
	resetAddModelGlobals(t)
	stubAddModelUploadSeams(t)
	completeModelRepoUploadAll = func(sessionIDs []string) ([]*api.CompleteModelRepoUploadResult, error) {
		out := make([]*api.CompleteModelRepoUploadResult, len(sessionIDs))
		for i, id := range sessionIDs {
			out[i] = &api.CompleteModelRepoUploadResult{Success: true, SessionID: id, Status: "FAILED"}
		}
		return out, nil
	}

	_, _, _, err := uploadModelFiles([]modelFile{{RelativePath: "weights.bin", Size: 1}}, &api.CreateModelRepoUploadInput{})
	if err == nil || !strings.Contains(err.Error(), "weights.bin") || !strings.Contains(err.Error(), "FAILED") {
		t.Fatalf("expected FAILED completion error naming the file, got %v", err)
	}
}

func TestValidateModelRelativePath(t *testing.T) {
	for _, bad := range []string{"a\\b.bin", "a\nb.bin", "a\tb", "a\x00b", "dir/\x1b[31mred", "a\u0085b"} {
		if err := validateModelRelativePath(bad); err == nil {
			t.Errorf("expected %q rejected", bad)
		}
	}
	for _, good := range []string{"weights.bin", "dir/sub/.env", "héllo wörld.bin", "日本語/モデル.bin"} {
		if err := validateModelRelativePath(good); err != nil {
			t.Errorf("expected %q accepted, got %v", good, err)
		}
	}
}

func TestCollectModelFiles_RejectsControlCharacterNames(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.bin"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+string(filepath.Separator)+"bad\nname.bin", []byte("x"), 0o600); err != nil {
		t.Skipf("filesystem rejects newline in name: %v", err)
	}
	if _, err := collectModelFiles(root); err == nil || !strings.Contains(err.Error(), "control character") {
		t.Fatalf("expected control-character rejection, got %v", err)
	}
}
