package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreatePod_IncludesMinCudaVersion(t *testing.T) {
	var gotMinCudaVersion string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input GraphQLInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		payload, _ := input.Variables["input"].(map[string]interface{})
		if value, ok := payload["minCudaVersion"].(string); ok {
			gotMinCudaVersion = value
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"podFindAndDeployOnDemand": map[string]interface{}{
					"id": "pod-1",
				},
			},
		})
	}))
	defer server.Close()

	client := &GraphQLClient{
		url:        server.URL,
		apiKey:     "test-key",
		httpClient: server.Client(),
		userAgent:  "test",
	}

	_, err := client.CreatePod(&CreatePodGQLInput{
		GpuCount:       1,
		GpuTypeId:      "NVIDIA GeForce RTX 4090",
		ImageName:      "runpod/test",
		MinCudaVersion: "12.6",
		StartSsh:       true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMinCudaVersion != "12.6" {
		t.Fatalf("expected minCudaVersion to be sent, got %q", gotMinCudaVersion)
	}
}
