package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

func TestEndpointHealthCounts(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		// the live wire shape, verbatim from prod.
		w.Write([]byte(`{
			"jobs":{"completed":27,"failed":0,"inProgress":0,"inQueue":0,"retried":0},
			"workers":{"idle":1,"initializing":0,"ready":1,"running":0,"throttled":0,"unhealthy":0}
		}`)) //nolint:errcheck
	}))
	defer server.Close()

	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv(configenv.APIKeyEnv, "test-key")
	// health lives on the invoke service, so it must follow RUNPOD_INVOKE_URL and
	// not the rest control plane url.
	t.Setenv(configenv.InvokeURLEnv, server.URL)

	client, err := NewInvokeClient()
	if err != nil {
		t.Fatalf("new invoke client: %v", err)
	}

	health, err := client.EndpointHealthCounts(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/ep-1/health" {
		t.Errorf("requested %q, want /ep-1/health", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("authorization = %q, want a bearer token", gotAuth)
	}
	if health.Workers.Ready != 1 || health.Workers.Idle != 1 {
		t.Errorf("unexpected worker counts: %+v", health.Workers)
	}
	if health.Jobs.Completed != 27 {
		t.Errorf("unexpected job counts: %+v", health.Jobs)
	}
}

func TestEndpointHealthCountsErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"endpoint not found"}`)) //nolint:errcheck
	}))
	defer server.Close()

	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv(configenv.APIKeyEnv, "test-key")
	t.Setenv(configenv.InvokeURLEnv, server.URL)

	client, err := NewInvokeClient()
	if err != nil {
		t.Fatalf("new invoke client: %v", err)
	}

	if _, err := client.EndpointHealthCounts(context.Background(), "ep-1"); err == nil {
		t.Fatal("expected an error")
	} else if apiErr, ok := err.(*APIError); !ok || apiErr.ErrorCode() != "not_found" {
		t.Fatalf("expected a not_found APIError, got %#v", err)
	}

	if _, err := client.EndpointHealthCounts(context.Background(), "  "); err == nil {
		t.Fatal("expected an error for a blank endpoint id")
	}
}
