package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	runpod "github.com/runpod/go-sdk/v2"
	"github.com/runpod/go-sdk/v2/lib/types"
)

func TestSDKRequestsKeepConfiguredBasePath(t *testing.T) {
	for _, tc := range []struct{ base, want string }{
		{"", "/network-volumes/vol-1"},
		{"/v2", "/v2/network-volumes/vol-1"},
		{"/proxy/v2/", "/proxy/v2/network-volumes/vol-1"},
	} {
		var got string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.Path
			_, _ = io.WriteString(w, `{"id":"vol-1","name":"v","size":10,"dataCenter":"EU-RO-1"}`)
		}))
		t.Setenv("RUNPOD_API_KEY", "test-key")
		t.Setenv("RUNPOD_API_URL", server.URL+tc.base)
		client, err := NewClient()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.GetNetworkVolume("vol-1"); err != nil {
			t.Fatalf("base %q: %v", tc.base, err)
		}
		server.Close()
		if got != tc.want {
			t.Errorf("base %q: path = %q, want %q", tc.base, got, tc.want)
		}
	}
}

func TestSDKRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		call     func(c *Client) error
		attempts int32
	}{
		{"get 503 is retried", http.StatusServiceUnavailable, func(c *Client) error { _, err := c.GetNetworkVolume("vol-1"); return err }, maxRetryAttempts},
		{"get 429 is retried", http.StatusTooManyRequests, func(c *Client) error { _, err := c.GetNetworkVolume("vol-1"); return err }, maxRetryAttempts},
		{"post 429 is not retried", http.StatusTooManyRequests, func(c *Client) error {
			_, err := c.CreateNetworkVolume(&NetworkVolumeCreateRequest{Name: "v", Size: 10, DataCenterID: "EU-RO-1"})
			return err
		}, 1},
		{"patch 503 is not retried", http.StatusServiceUnavailable, func(c *Client) error {
			_, err := c.UpdateNetworkVolume("vol-1", &NetworkVolumeUpdateRequest{Name: "v"})
			return err
		}, 1},
		{"delete 429 is not retried", http.StatusTooManyRequests, func(c *Client) error { return c.DeleteNetworkVolume("vol-1") }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withFastRetry(t)
			var n atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(server.Close)

			var apiErr *APIError
			if err := tc.call(newV2TestClient(t, server)); !errors.As(err, &apiErr) || apiErr.Status != tc.status {
				t.Fatalf("err = %v, want an APIError with status %d", err, tc.status)
			}
			if got := n.Load(); got != tc.attempts {
				t.Fatalf("attempts = %d, want %d", got, tc.attempts)
			}
		})
	}
}

func TestSDKRedirectIsNotFollowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer server.Close()

	err := newV2TestClient(t, server).DeleteNetworkVolume("vol-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusMovedPermanently {
		t.Fatalf("err = %v, want an APIError naming the redirect", err)
	}
}

func TestRequireIDRejectsEmptyAndDotSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL.Path)
	}))
	defer server.Close()
	client := newV2TestClient(t, server)

	for _, id := range []string{"", ".", "..", "a/b"} {
		var coder interface{ ErrorCode() string }
		if err := client.DeleteNetworkVolume(id); !errors.As(err, &coder) || coder.ErrorCode() != "usage_error" {
			t.Errorf("id %q: err = %v, want usage_error", id, err)
		}
	}
}

func TestCreateTemplateLeavesOutEmptyNameAndImage(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"title":"Unprocessable Entity","status":422,"detail":"Request validation failed."}`)
	}))
	defer server.Close()

	_, _ = newV2TestClient(t, server).CreateTemplate(&TemplateCreateRequest{Name: "", ImageName: ""})
	if _, ok := body["name"]; ok {
		t.Errorf("body has name: %v", body)
	}
	if _, ok := body["image"]; ok {
		t.Errorf("body has image: %v", body)
	}
	if body["serverless"] != false {
		t.Errorf("body lost its other fields: %v", body)
	}
}

func TestSDKAllPagesFollowsCursorsAndStopsOnRepeat(t *testing.T) {
	pages := map[string]types.Pagination{
		"":    {HasNextPage: true, NextCursor: runpod.Ptr("a b")},
		"a b": {HasNextPage: false},
	}
	var seen []string
	fetch := func(cursor *string) (types.Pagination, error) {
		c := deref(cursor)
		seen = append(seen, c)
		return pages[c], nil
	}
	if err := sdkAllPages("/things", fetch); err != nil || !reflect.DeepEqual(seen, []string{"", "a b"}) {
		t.Fatalf("err = %v, cursors = %q", err, seen)
	}

	pages["a b"] = types.Pagination{HasNextPage: true, NextCursor: runpod.Ptr("a b")}
	seen = nil
	if err := sdkAllPages("/things", fetch); err == nil || len(seen) != 2 {
		t.Fatalf("a repeated cursor must stop the walk with an error: err = %v, cursors = %q", err, seen)
	}

	pages[""] = types.Pagination{HasNextPage: true}
	if err := sdkAllPages("/things", fetch); err == nil || !strings.Contains(err.Error(), "no cursor") {
		t.Fatalf("more pages without a cursor must be an error, not a partial list: %v", err)
	}
}

func TestSDKListSendsTheCursorEscaped(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("cursor"))
		if r.URL.Query().Get("cursor") == "" {
			_, _ = io.WriteString(w, `{"networkVolumes":[],"pagination":{"hasNextPage":true,"nextCursor":"a b&c"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"networkVolumes":[],"pagination":{"hasNextPage":false}}`)
	}))
	defer server.Close()

	if _, err := newV2TestClient(t, server).ListNetworkVolumes(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(queries, []string{"", "a b&c"}) {
		t.Fatalf("cursors = %q", queries)
	}
}

// ids are path-escaped by the generated client, so an encoded dot or slash
// reaches the API as the literal text, never as a dot segment
func TestSDKEscapesEncodedDotsInIDs(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newV2TestClient(t, server)

	for _, id := range []string{"%2e", "%2e%2e", "a%2fb"} {
		if err := client.DeleteNetworkVolume(id); err != nil {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	want := []string{"/network-volumes/%252e", "/network-volumes/%252e%252e", "/network-volumes/a%252fb"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %q, want %q", paths, want)
	}
}
