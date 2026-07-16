package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestListGpuTypes_PricingAndPerDC verifies that gpu list carries on-demand
// pricing straight through and derives both the best overall stock status and
// the per-data-center breakdown from the dataCenters query.
func TestListGpuTypes_PricingAndPerDC(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := string(body)
		switch {
		case strings.Contains(q, "gpuTypes"):
			w.Write([]byte(`{"data":{"gpuTypes":[
				{"id":"NVIDIA A40","displayName":"A40","memoryInGb":48,"secureCloud":true,"communityCloud":true,"securePrice":0.39,"communityPrice":0.29},
				{"id":"NVIDIA GeForce RTX 4090","displayName":"RTX 4090","memoryInGb":24,"secureCloud":false,"communityCloud":true,"securePrice":0,"communityPrice":0.69},
				{"id":"unknown","displayName":"unknown","memoryInGb":0}
			]}}`))
		case strings.Contains(q, "dataCenters"):
			w.Write([]byte(`{"data":{"dataCenters":[
				{"id":"US-GA-1","name":"Georgia","location":"US","gpuAvailability":[{"gpuTypeId":"NVIDIA A40","displayName":"A40","stockStatus":"Low"}]},
				{"id":"EU-RO-1","name":"Romania","location":"EU","gpuAvailability":[{"gpuTypeId":"NVIDIA A40","displayName":"A40","stockStatus":"High"},{"gpuTypeId":"NVIDIA GeForce RTX 4090","displayName":"RTX 4090","stockStatus":"Medium"}]},
				{"id":"US-KS-2","name":"Kansas","location":"US","gpuAvailability":[{"gpuTypeId":"NVIDIA A40","displayName":"A40","stockStatus":""}]}
			]}}`))
		default:
			t.Errorf("unexpected graphql query: %s", q)
		}
	}))
	defer server.Close()

	viper.Set("apiUrl", server.URL)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	gpus, err := client.ListGpuTypes(false)
	if err != nil {
		t.Fatalf("ListGpuTypes: %v", err)
	}

	byID := map[string]GpuTypeWithAvailability{}
	for _, g := range gpus {
		byID[g.ID] = g
	}

	a40, ok := byID["NVIDIA A40"]
	if !ok {
		t.Fatal("expected A40 in results")
	}
	if a40.SecurePrice != 0.39 || a40.CommunityPrice != 0.29 {
		t.Errorf("A40 pricing = %v/%v, want 0.39/0.29", a40.SecurePrice, a40.CommunityPrice)
	}
	if a40.StockStatus != "High" {
		t.Errorf("A40 best stock = %q, want High", a40.StockStatus)
	}
	if len(a40.DataCenterAvailability) != 3 {
		t.Fatalf("A40 per-dc availability len = %d, want 3", len(a40.DataCenterAvailability))
	}
	seen := map[string]string{}
	for _, dc := range a40.DataCenterAvailability {
		seen[dc.DataCenterID] = dc.StockStatus
	}
	// every dc the gpu appears in is listed; an unreported status is "none",
	// never an empty string.
	if seen["US-GA-1"] != "Low" || seen["EU-RO-1"] != "High" || seen["US-KS-2"] != "none" {
		t.Errorf("A40 per-dc availability = %+v", a40.DataCenterAvailability)
	}

	if _, ok := byID["unknown"]; ok {
		t.Error("the 'unknown' gpu type should be filtered out")
	}
}
