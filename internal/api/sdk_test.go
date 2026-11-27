package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
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
