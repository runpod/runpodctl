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

func TestStockRank(t *testing.T) {
	// the known set, pinned: if the api adds a level, this test should be the
	// thing that fails, not a silently misranked gpu in `gpu list`.
	if len(stockOrder) != 3 {
		t.Errorf("stockOrder has %d entries; a new api stock level must be ranked explicitly, not left to fall through to the unknown bucket", len(stockOrder))
	}

	if got, want := stockRank(""), 0; got != want {
		t.Errorf("stockRank(\"\") = %d, want %d", got, want)
	}
	// case and whitespace must not change the ranking.
	for _, s := range []string{"High", "high", "HIGH", " High "} {
		if stockRank(s) != stockRank("High") {
			t.Errorf("stockRank(%q) = %d, want same as \"High\" (%d)", s, stockRank(s), stockRank("High"))
		}
	}
	if !(stockRank("High") > stockRank("Medium") && stockRank("Medium") > stockRank("Low")) {
		t.Error("expected High > Medium > Low")
	}

	// the regression this fixes: an unknown status used to score 0 and tie with
	// "no stock", so it lost to Low and the top-level stockStatus under-reported
	// a gpu that was actually available.
	if !betterStock("Very High", "") {
		t.Error("an unknown non-empty status must outrank an absent one")
	}
	if betterStock("", "Low") {
		t.Error("an absent status must never outrank Low")
	}
	if !betterStock("Low", "Very High") {
		t.Error("a known level should still win over an unorderable unknown (documented tradeoff)")
	}
}
