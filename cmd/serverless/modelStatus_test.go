package serverless

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

// withModelStatusServer runs one test server that answers both the GraphQL
// endpoint (api.GetEndpointModelReferences, api.GetPodModelVersions) and the
// v2 REST worker listing (internalapi.ListEndpointWorkersWithTimeout), routed
// by method/path -- buildModelStatusResult calls both in the same request.
func withModelStatusServer(t *testing.T, graphqlHandler http.HandlerFunc, restHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			graphqlHandler(w, r)
			return
		}
		restHandler(w, r)
	}))
	t.Setenv(configenv.GraphQLURLEnv, server.URL)
	t.Setenv(configenv.RESTV2URLEnv, server.URL)
	t.Setenv(configenv.APIKeyEnv, "test-key")
	previousTimeout := viper.Get("timeout")
	viper.Set("timeout", 0)
	t.Cleanup(func() { viper.Set("timeout", previousTimeout) })
	t.Cleanup(server.Close)
	return server
}

func TestModelStatusCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range Cmd.Commands() {
		if cmd.Use == "model-status <endpoint-id>" {
			found = true
		}
	}
	if !found {
		t.Error("serverless model-status is not registered on the serverless command")
	}
}

func TestBuildModelStatusResultAssemblesReferencesAndPerWorkerDiagnostics(t *testing.T) {
	withModelStatusServer(t,
		func(w http.ResponseWriter, r *http.Request) {
			var input api.Input
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatalf("decode graphql request: %v", err)
			}
			switch {
			case strings.Contains(input.Query, "EndpointModelReferences"):
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"myself": map[string]interface{}{
							"endpoint": map[string]interface{}{
								"modelReferences": []string{"https://huggingface.co/org/model:main"},
							},
						},
					},
				})
			case strings.Contains(input.Query, "PodModelVersions"):
				podID, _ := input.Variables["podId"].(string)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"pod": map[string]interface{}{
							"modelVersions": []map[string]interface{}{
								{
									"modelVersionUuid":        "uuid-" + podID,
									"modelVersionHash":        "hash-" + podID,
									"machineAssignmentStatus": "DEPLOYED",
									"failurePhase":            nil,
									"failureReason":           nil,
									"mountPath":               "/runpod/model-store/huggingface/org/model/hash-" + podID,
								},
							},
						},
					},
				})
			default:
				t.Fatalf("unexpected graphql query: %s", input.Query)
			}
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/serverless/ep1/workers" {
				t.Fatalf("unexpected rest path %q", r.URL.Path)
			}
			fmt.Fprint(w, `{"workers":[{"id":"w1","status":"RUNNING"},{"id":"w2","status":"IDLE"}]}`)
		},
	)

	result, err := buildModelStatusResult("ep1")
	if err != nil {
		t.Fatalf("buildModelStatusResult returned error: %v", err)
	}

	if result.EndpointID != "ep1" {
		t.Errorf("endpointId = %q, want ep1", result.EndpointID)
	}
	if len(result.ModelReferences) != 1 || result.ModelReferences[0] != "https://huggingface.co/org/model:main" {
		t.Fatalf("unexpected model references: %#v", result.ModelReferences)
	}
	if len(result.Workers) != 2 {
		t.Fatalf("workers = %d, want 2", len(result.Workers))
	}
	for i, wantID := range []string{"w1", "w2"} {
		worker := result.Workers[i]
		if worker.WorkerID != wantID {
			t.Errorf("worker %d id = %q, want %q", i, worker.WorkerID, wantID)
		}
		if worker.Error != "" {
			t.Errorf("worker %d unexpected error: %s", i, worker.Error)
		}
		if len(worker.ModelVersions) != 1 {
			t.Fatalf("worker %d modelVersions = %d, want 1", i, len(worker.ModelVersions))
		}
		if worker.ModelVersions[0].ModelVersionHash != "hash-"+wantID {
			t.Errorf("worker %d hash = %q", i, worker.ModelVersions[0].ModelVersionHash)
		}
	}
}

// A worker whose model-version lookup fails must not blank out the other
// workers' diagnostics in the same response -- the error is reported inline
// on that one worker instead.
func TestBuildModelStatusResultOneWorkerFailureDoesNotBlankOthers(t *testing.T) {
	withModelStatusServer(t,
		func(w http.ResponseWriter, r *http.Request) {
			var input api.Input
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatalf("decode graphql request: %v", err)
			}
			switch {
			case strings.Contains(input.Query, "EndpointModelReferences"):
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"myself": map[string]interface{}{
							"endpoint": map[string]interface{}{"modelReferences": []string{}},
						},
					},
				})
			case strings.Contains(input.Query, "PodModelVersions"):
				podID, _ := input.Variables["podId"].(string)
				if podID == "w-bad" {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"errors": []map[string]interface{}{{"message": "pod not found"}},
					})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"pod": map[string]interface{}{"modelVersions": []map[string]interface{}{}},
					},
				})
			}
		},
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"workers":[{"id":"w-bad","status":"UNHEALTHY"},{"id":"w-good","status":"IDLE"}]}`)
		},
	)

	result, err := buildModelStatusResult("ep1")
	if err != nil {
		t.Fatalf("buildModelStatusResult returned error: %v", err)
	}
	if len(result.Workers) != 2 {
		t.Fatalf("workers = %d, want 2", len(result.Workers))
	}
	if result.Workers[0].Error == "" {
		t.Error("expected the bad worker to carry an inline error")
	}
	if result.Workers[1].Error != "" {
		t.Errorf("expected the good worker to be unaffected, got error %q", result.Workers[1].Error)
	}
}

func TestBuildModelStatusResultSkipsWorkersWithoutIDs(t *testing.T) {
	withModelStatusServer(t,
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"myself": map[string]interface{}{
						"endpoint": map[string]interface{}{"modelReferences": []string{}},
					},
				},
			})
		},
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"workers":[{"id":"","status":"UNKNOWN"}]}`)
		},
	)

	result, err := buildModelStatusResult("ep1")
	if err != nil {
		t.Fatalf("buildModelStatusResult returned error: %v", err)
	}
	if len(result.Workers) != 0 {
		t.Fatalf("expected workers without an id to be skipped, got %#v", result.Workers)
	}
}

func TestBuildModelStatusResultPropagatesEndpointLookupError(t *testing.T) {
	withModelStatusServer(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("must not list workers if the model-references lookup already failed")
		},
	)

	if _, err := buildModelStatusResult("ep1"); err == nil {
		t.Fatal("expected an error when the model-references lookup fails")
	}
}
