package serverless

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runpod/runpodctl/internal/configenv"
)

func withEnvServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Setenv(configenv.GraphQLURLEnv, server.URL)
	t.Setenv(configenv.APIKeyEnv, "test-key")
	t.Cleanup(server.Close)
	return server
}

func TestEnvCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range Cmd.Commands() {
		if cmd.Use == "env <endpoint-id>" {
			found = true
		}
	}
	if !found {
		t.Error("serverless env is not registered on the serverless command")
	}
}

func TestBuildEndpointEnvResultReturnsConfiguredOverride(t *testing.T) {
	withEnvServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"myself": map[string]interface{}{
					"endpoint": map[string]interface{}{
						"env": []map[string]interface{}{
							{"key": "MODEL_NAME", "value": "org/model"},
						},
					},
				},
			},
		})
	})

	result, err := buildEndpointEnvResult("ep1")
	if err != nil {
		t.Fatalf("buildEndpointEnvResult returned error: %v", err)
	}
	if result.EndpointID != "ep1" {
		t.Errorf("endpointId = %q, want ep1", result.EndpointID)
	}
	if len(result.EnvOverride) != 1 || result.EnvOverride[0].Key != "MODEL_NAME" || result.EnvOverride[0].Value != "org/model" {
		t.Fatalf("unexpected env override: %#v", result.EnvOverride)
	}
}

func TestBuildEndpointEnvResultEmptyForNoOverride(t *testing.T) {
	withEnvServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"myself": map[string]interface{}{
					"endpoint": map[string]interface{}{
						"env": []map[string]interface{}{},
					},
				},
			},
		})
	})

	result, err := buildEndpointEnvResult("ep1")
	if err != nil {
		t.Fatalf("buildEndpointEnvResult returned error: %v", err)
	}
	if len(result.EnvOverride) != 0 {
		t.Fatalf("expected no env override, got %#v", result.EnvOverride)
	}
}

func TestBuildEndpointEnvResultPropagatesLookupError(t *testing.T) {
	withEnvServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := buildEndpointEnvResult("ep1"); err == nil {
		t.Fatal("expected an error when the env override lookup fails")
	}
}
