package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/viper"
)

func TestGetEndpointEnvOverrideReturnsConfiguredOverride(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	var gotID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input Input
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if id, ok := input.Variables["id"].(string); ok {
			gotID = id
		}

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
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	env, err := GetEndpointEnvOverride("endpoint-1")
	if err != nil {
		t.Fatalf("GetEndpointEnvOverride returned error: %v", err)
	}
	if gotID != "endpoint-1" {
		t.Fatalf("expected id variable endpoint-1, got %q", gotID)
	}
	if len(env) != 1 || env[0].Key != "MODEL_NAME" || env[0].Value != "org/model" {
		t.Fatalf("unexpected env: %#v", env)
	}
}

func TestGetEndpointEnvOverrideEmptyForNoOverride(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"myself": map[string]interface{}{
					"endpoint": map[string]interface{}{
						"env": []map[string]interface{}{},
					},
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	env, err := GetEndpointEnvOverride("endpoint-no-override")
	if err != nil {
		t.Fatalf("GetEndpointEnvOverride returned error: %v", err)
	}
	if len(env) != 0 {
		t.Fatalf("expected no env override, got %#v", env)
	}
}

func TestGetEndpointEnvOverrideEmptyIDIsRejectedLocally(t *testing.T) {
	if _, err := GetEndpointEnvOverride("  "); err == nil {
		t.Fatal("expected an error for an empty endpoint id")
	}
}

func TestGetEndpointEnvOverrideNotFound(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"myself": map[string]interface{}{
					"endpoint": nil,
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	if _, err := GetEndpointEnvOverride("missing-endpoint"); err == nil {
		t.Fatal("expected an error when the endpoint is not found")
	}
}

func TestGetEndpointEnvOverrideGraphQLErrorSurfaces(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{{"message": "endpoint not found"}},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	_, err := GetEndpointEnvOverride("gone")
	if err == nil {
		t.Fatal("expected an error to surface from the graphql response")
	}
	if err.Error() != "endpoint not found" {
		t.Fatalf("unexpected error: %v", err)
	}
}
