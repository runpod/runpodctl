package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateSpotPod_IncludesBidPerGPU(t *testing.T) {
	var gotBid float64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input GraphQLInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		payload, _ := input.Variables["input"].(map[string]interface{})
		if value, ok := payload["bidPerGpu"].(float64); ok {
			gotBid = value
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"podRentInterruptable": map[string]interface{}{
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

	_, err := client.CreateSpotPod(&CreatePodGQLInput{
		BidPerGpu: 0.2,
		GpuCount:  1,
		GpuTypeId: "NVIDIA GeForce RTX 4090",
		ImageName: "ubuntu:22.04",
		StartSsh:  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBid != 0.2 {
		t.Fatalf("expected bidPerGpu 0.2, got %v", gotBid)
	}
}
