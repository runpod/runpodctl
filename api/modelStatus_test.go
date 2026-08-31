package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/viper"
)

func TestGetEndpointModelReferencesReturnsConfiguredReferences(t *testing.T) {
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
						"modelReferences": []string{"https://huggingface.co/org/model:main"},
					},
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	refs, err := GetEndpointModelReferences("endpoint-1")
	if err != nil {
		t.Fatalf("GetEndpointModelReferences returned error: %v", err)
	}
	if gotID != "endpoint-1" {
		t.Fatalf("expected id variable endpoint-1, got %q", gotID)
	}
	if len(refs) != 1 || refs[0] != "https://huggingface.co/org/model:main" {
		t.Fatalf("unexpected references: %#v", refs)
	}
}

func TestGetEndpointModelReferencesEmptyIDIsRejectedLocally(t *testing.T) {
	if _, err := GetEndpointModelReferences("  "); err == nil {
		t.Fatal("expected an error for an empty endpoint id")
	}
}

func TestGetEndpointModelReferencesNotFound(t *testing.T) {
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

	if _, err := GetEndpointModelReferences("missing-endpoint"); err == nil {
		t.Fatal("expected an error when the endpoint is not found")
	}
}

func TestGetPodModelVersionsReturnsDiagnostics(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	var gotPodID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input Input
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if id, ok := input.Variables["podId"].(string); ok {
			gotPodID = id
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"pod": map[string]interface{}{
					"modelVersions": []map[string]interface{}{
						{
							"modelVersionUuid":        "uuid-a",
							"modelVersionHash":        "hash-a",
							"machineAssignmentStatus": "FAILED",
							"failurePhase":            "download_failed",
							"failureReason":           "2 of 5 files failed to download",
							"mountPath":               "/runpod/model-store/huggingface/org/model/hash-a",
						},
					},
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	versions, err := GetPodModelVersions("worker-1")
	if err != nil {
		t.Fatalf("GetPodModelVersions returned error: %v", err)
	}
	if gotPodID != "worker-1" {
		t.Fatalf("expected podId variable worker-1, got %q", gotPodID)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 model version, got %d", len(versions))
	}
	v := versions[0]
	if v.ModelVersionHash != "hash-a" {
		t.Errorf("modelVersionHash = %q, want hash-a", v.ModelVersionHash)
	}
	if v.Status == nil || *v.Status != "FAILED" {
		t.Errorf("status = %v, want FAILED", v.Status)
	}
	if v.FailurePhase == nil || *v.FailurePhase != "download_failed" {
		t.Errorf("failurePhase = %v, want download_failed", v.FailurePhase)
	}
	if v.FailureReason == nil || *v.FailureReason != "2 of 5 files failed to download" {
		t.Errorf("failureReason = %v, want a non-empty reason", v.FailureReason)
	}
	if v.MountPath == nil || *v.MountPath != "/runpod/model-store/huggingface/org/model/hash-a" {
		t.Errorf("mountPath = %v, want the expected mount path", v.MountPath)
	}
}

func TestGetPodModelVersionsEmptyForNoModelDependency(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"pod": map[string]interface{}{
					"modelVersions": []map[string]interface{}{},
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	versions, err := GetPodModelVersions("worker-no-model")
	if err != nil {
		t.Fatalf("GetPodModelVersions returned error: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("expected no model versions, got %#v", versions)
	}
}

func TestGetPodModelVersionsEmptyIDIsRejectedLocally(t *testing.T) {
	if _, err := GetPodModelVersions(""); err == nil {
		t.Fatal("expected an error for an empty pod id")
	}
}

func TestGetPodModelVersionsGraphQLErrorSurfaces(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{{"message": "pod not found"}},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	_, err := GetPodModelVersions("gone")
	if err == nil {
		t.Fatal("expected an error to surface from the graphql response")
	}
	if err.Error() != "pod not found" {
		t.Fatalf("unexpected error: %v", err)
	}
}
