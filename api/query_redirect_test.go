package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/viper"
)

// TestQueryRefusesRedirectAndKeepsAPIKeyOffSecondListener pins that a 307 to the
// same hostname on another port (same-site, so go would keep the Authorization
// header) is not followed: the second listener sees no request at all, and the
// caller gets an error rather than a 3xx it could mistake for a result.
func TestQueryRefusesRedirectAndKeepsAPIKeyOffSecondListener(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "super-secret-key")

	var sawAuth atomic.Value
	var secondHits atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		sawAuth.Store(r.Header.Get("Authorization"))
		if err := json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer second.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL, http.StatusTemporaryRedirect)
	}))
	defer first.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", first.URL)

	res, err := Query(Input{Query: "query test"})
	if err == nil {
		if closeErr := res.Body.Close(); closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}
		t.Fatalf("expected an error for a redirected graphql request, got status %d", res.StatusCode)
	}
	if !errors.Is(err, ErrRedirectRefused) {
		t.Fatalf("expected ErrRedirectRefused, got %v", err)
	}
	if got := secondHits.Load(); got != 0 {
		t.Fatalf("redirect target received %d request(s) (Authorization=%v); expected none", got, sawAuth.Load())
	}
}

// TestModelRepoCallSurfacesRedirectAsError pins the full path: a model-repo call
// against a redirecting endpoint fails instead of reporting success.
func TestModelRepoCallSurfacesRedirectAsError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.invalid/graphql", http.StatusFound)
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	if _, err := CompleteModelRepoUploadBatch([]string{"session-a"}); !errors.Is(err, ErrRedirectRefused) {
		t.Fatalf("expected ErrRedirectRefused, got %v", err)
	}
}

// TestModelRepoResponseOverLimitIsError pins that a response larger than the
// bound fails rather than being buffered whole or silently truncated.
func TestModelRepoResponseOverLimitIsError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	chunk := []byte(strings.Repeat("x", 1<<20))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(`{"data":{"pad":"`)); err != nil {
			return
		}
		for range (maxModelResponseBytes >> 20) + 2 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
		// the client has usually hung up by now; nothing to report.
		if _, err := w.Write([]byte(`"}}`)); err != nil {
			return
		}
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	_, err := CompleteModelRepoUploadBatch([]string{"session-a"})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected an over-limit error, got %v", err)
	}
}

func TestReadBoundedResponseAtLimitSucceeds(t *testing.T) {
	data, err := ReadBoundedResponse(strings.NewReader(strings.Repeat("a", maxModelResponseBytes)))
	if err != nil || len(data) != maxModelResponseBytes {
		t.Fatalf("expected exactly-at-limit body to be read, got len=%d err=%v", len(data), err)
	}
}

// TestGraphQLCallsRejectOversizeResponses covers one representative of every
// file that reads a graphql response body, so a new unbounded read in any of
// them shows up here.
func TestGraphQLCallsRejectOversizeResponses(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	chunk := []byte(strings.Repeat("x", 1<<20))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(`{"data":{"pad":"`)); err != nil {
			return
		}
		for range (maxModelResponseBytes >> 20) + 2 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	for name, call := range map[string]func() error{
		"pod.go":         func() error { _, err := GetPods(); return err },
		"volume.go":      func() error { _, err := GetNetworkVolumes(); return err },
		"endpoint.go":    func() error { _, err := GetEndpoints(); return err },
		"envOverride.go": func() error { _, err := GetEndpointEnvOverride("ep"); return err },
		"modelStatus.go": func() error { _, err := GetEndpointModelReferences("ep"); return err },
		"modelStatus.go pod versions": func() error {
			_, err := GetPodModelVersions("pod")
			return err
		},
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Errorf("%s: expected an over-limit error, got %v", name, err)
		}
	}
}
