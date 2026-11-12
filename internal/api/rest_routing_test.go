package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestRESTClientsShareBaseURL(t *testing.T) {
	for _, tc := range []struct {
		name, env, config, want string
	}{
		{"default", "", "", "https://api.runpod.io/v2"},
		{"config", "", "https://config.example.test/v2/", "https://config.example.test/v2"},
		{"environment", "https://env.example.test/v2///", "https://config.example.test/v2", "https://env.example.test/v2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Setenv("RUNPOD_API_KEY", "test-key")
			t.Setenv("RUNPOD_API_URL", tc.env)
			t.Setenv("RUNPOD_REST_V2_URL", "https://unused.example.test/v2")
			t.Setenv("RUNPOD_GRAPHQL_URL", "")
			t.Setenv("RUNPOD_INVOKE_URL", "")
			viper.Set("restApiUrl", tc.config)
			viper.Set("restV2ApiUrl", "https://unused-config.example.test/v2")
			client, err := NewClient()
			if err != nil {
				t.Fatal(err)
			}
			workers, err := NewV2Client()
			if err != nil {
				t.Fatal(err)
			}
			logs, err := NewLogClient()
			if err != nil {
				t.Fatal(err)
			}
			for name, got := range map[string]string{"crud": client.baseURL, "workers": workers.baseURL, "logs": logs.baseURL} {
				if got != tc.want {
					t.Errorf("%s base = %q, want %q", name, got, tc.want)
				}
			}
			gql, err := NewGraphQLClient()
			if err != nil {
				t.Fatal(err)
			}
			invoke, err := NewInvokeClient()
			if err != nil {
				t.Fatal(err)
			}
			if gql.url != DefaultGraphQLURL || invoke.baseURL != ServerlessInvokeBaseURL {
				t.Fatalf("REST setting moved another service: graphql=%s invoke=%s", gql.url, invoke.baseURL)
			}
		})
	}
}

func TestRESTOverrideRoutesEveryClient(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing authorization")
		}
		switch r.URL.Path {
		case "/v2/catalog/gpus":
			writeBody(t, w, `{"gpus":[]}`)
		case "/v2/pods":
			writeBody(t, w, `{"pods":[]}`)
		case "/v2/serverless":
			writeBody(t, w, `{"endpoints":[]}`)
		case "/v2/serverless/ep-1":
			writeBody(t, w, `{"id":"ep-1","workers":{"min":0,"max":0}}`)
		case "/v2/serverless/ep-1/workers":
			writeBody(t, w, `{"workers":[]}`)
		case "/v2/pods/pod-1/logs", "/v2/serverless/ep-1/workers/worker-1/logs":
			w.Header().Set("Content-Type", "text/event-stream")
			writeBody(t, w, "data: {\"source\":\"system\",\"line\":\"routed\",\"ts\":\"2026-11-11T00:00:00Z\"}\n\n")
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_API_URL", server.URL+"/v2/")
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.ListGpuTypes(false); err != nil {
		t.Fatal(err)
	}
	if _, err = client.ListPods(nil); err != nil {
		t.Fatal(err)
	}
	if _, err = client.ListEndpoints(nil); err != nil {
		t.Fatal(err)
	}
	if err = client.UpdateEndpoint("ep-1", &EndpointUpdateRequest{Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
	if _, err = client.GetEndpoint("ep-1", false, true); err != nil {
		t.Fatal(err)
	}
	workers, err := NewV2Client()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = workers.ListEndpointWorkers(context.Background(), "ep-1"); err != nil {
		t.Fatal(err)
	}
	logs, err := NewLogClient()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{PodLogsPath("pod-1"), WorkerLogsPath("ep-1", "worker-1")} {
		var entries []LogEntry
		if err := logs.Snapshot(context.Background(), path, LogStreamOptions{}, time.Second, func(entry LogEntry) error {
			entries = append(entries, entry)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Line != "routed" {
			t.Fatalf("unexpected logs: %v", entries)
		}
	}
	want := []string{
		"GET /v2/catalog/gpus", "GET /v2/pods", "GET /v2/serverless",
		"PATCH /v2/serverless/ep-1", "GET /v2/serverless/ep-1",
		"GET /v2/serverless/ep-1/workers", "GET /v2/serverless/ep-1/workers",
		"GET /v2/pods/pod-1/logs", "GET /v2/serverless/ep-1/workers/worker-1/logs",
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %v, want %v", requests, want)
	}
}

func TestGraphQLRequestDoesNotChangeRESTBase(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	gql := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" || r.Method != http.MethodPost {
			t.Errorf("unexpected graphql request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		writeBody(t, w, `{"data":{}}`)
	}))
	t.Cleanup(gql.Close)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/pods" || r.Method != http.MethodGet {
			t.Errorf("unexpected rest request: %s %s", r.Method, r.URL.Path)
		}
		writeBody(t, w, `{"pods":[]}`)
	}))
	t.Cleanup(rest.Close)
	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_API_URL", rest.URL+"/v2")
	t.Setenv("RUNPOD_GRAPHQL_URL", gql.URL+"/graphql")
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := client.graphqlRequest("query { myself { id } }", nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("graphql request did not reach its own host")
	}
	if client.baseURL != rest.URL+"/v2" {
		t.Fatalf("graphql changed the REST base to %s", client.baseURL)
	}
	if _, err := client.ListPods(nil); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
