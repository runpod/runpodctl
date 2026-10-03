package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// TestListGpuTypes_PricingAndPerDC verifies that gpu list carries on-demand
// pricing straight through from the v2 catalog, spells availability the way
// the cli always has (High, not HIGH), and keeps the per-data-center breakdown.
func TestListGpuTypes_PricingAndPerDC(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog/gpus" || r.URL.Query().Get("include") != "AVAILABILITY" || r.URL.Query().Get("product") != "POD" {
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"gpus":[
			{"id":"NVIDIA A40","name":"A40","memory":48,"secure":true,"community":true,"pool":"AMPERE_48",
			 "price":{"secure":0.39,"community":0.29},"availability":"HIGH",
			 "dataCenters":[{"id":"US-GA-1","availability":"LOW"},{"id":"EU-RO-1","availability":"HIGH"},{"id":"US-KS-2","availability":"NONE"}]},
			{"id":"NVIDIA GeForce RTX 4090","name":"RTX 4090","memory":24,"secure":false,"community":true,"pool":"ADA_24",
			 "price":{"secure":0,"community":0.69},"availability":"NONE","dataCenters":[]},
			{"id":"NVIDIA RTX A4000","name":"RTX A4000","memory":16,"secure":true,"availability":"NONE",
			 "dataCenters":[{"id":"EUR-IS-1","availability":"LOW"}]},
			{"id":"unknown","name":"unknown","memory":0,"availability":"LOW"}
		]}`)
	}))
	defer server.Close()

	gpus, err := newV2TestClient(t, server).ListGpuTypes(false)
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
	if a40.DisplayName != "A40" || a40.MemoryInGb != 48 || !a40.SecureCloud || !a40.CommunityCloud {
		t.Errorf("A40 = %+v", a40.GpuType)
	}
	if a40.SecurePrice != 0.39 || a40.CommunityPrice != 0.29 {
		t.Errorf("A40 pricing = %v/%v, want 0.39/0.29", a40.SecurePrice, a40.CommunityPrice)
	}
	if a40.StockStatus != "High" || !a40.Available {
		t.Errorf("A40 stock = %q available=%v, want High/true", a40.StockStatus, a40.Available)
	}
	seen := map[string]string{}
	for _, dc := range a40.DataCenterAvailability {
		seen[dc.DataCenterID] = dc.StockStatus
	}
	// a NONE entry is "none", never an empty string.
	if len(seen) != 3 || seen["US-GA-1"] != "Low" || seen["EU-RO-1"] != "High" || seen["US-KS-2"] != "none" {
		t.Errorf("A40 per-dc availability = %+v", a40.DataCenterAvailability)
	}
	if _, ok := byID["NVIDIA GeForce RTX 4090"]; ok {
		t.Error("a gpu with no stock should be filtered out without --include-unavailable")
	}
	if _, ok := byID["unknown"]; ok {
		t.Error("the 'unknown' gpu type should be filtered out")
	}
	// v2's top level can read NONE while a data center has stock; the data
	// center wins, so the gpu is not hidden
	if a4000 := byID["NVIDIA RTX A4000"]; a4000.StockStatus != "Low" || !a4000.Available {
		t.Errorf("A4000 stock = %q available=%v, want Low/true from its data center", a4000.StockStatus, a4000.Available)
	}
}

func TestListServerlessGpuPoolsGroupsCatalogByPool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"gpus":[
			{"id":"NVIDIA A40","pool":"AMPERE_48"},{"id":"NVIDIA RTX A6000","pool":"AMPERE_48"},
			{"id":"NVIDIA GeForce RTX 4090","pool":"ADA_24"},{"id":"NVIDIA B200","pool":""}]}`)
	}))
	defer server.Close()

	client := newV2TestClient(t, server)
	got, err := client.ResolveServerlessGpuPoolID("NVIDIA RTX A6000, ADA_24,NVIDIA A40")
	if err != nil {
		t.Fatalf("ResolveServerlessGpuPoolID: %v", err)
	}
	if got != "AMPERE_48,ADA_24" {
		t.Fatalf("resolved = %q, want AMPERE_48,ADA_24", got)
	}
	if _, err := client.ResolveServerlessGpuPoolID("NVIDIA B200"); err == nil {
		t.Fatal("a gpu with no pool must not resolve")
	}
}

// A failed pools query used to be swallowed: the input came back unchanged with
// a nil error, so a gpu *type* id sailed through untranslated and saveEndpoint
// rejected the write with `Invalid GPU Pool ID`, hiding the outage that caused
// it (found live on PR #340).
func TestResolveServerlessGpuPoolID_PoolQueryFailurePropagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog/gpus" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	got, err := newV2TestClient(t, server).ResolveServerlessGpuPoolID("NVIDIA A40")
	if err == nil {
		t.Fatal("expected an error when the pools query fails")
	}
	if !strings.Contains(err.Error(), "failed to look up serverless gpu pools") {
		t.Errorf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("resolved = %q, want empty; an unresolved gpu type id must never reach a caller", got)
	}
}

func TestListDataCentersCarriesRegionAndLegacyStock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog/datacenters" || r.URL.Query().Get("include") != "GPU_AVAILABILITY" {
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"dataCenters":[{"id":"AP-IN-1","name":"AP-IN-1","region":"ASIA",
			"gpuAvailability":[{"id":"NVIDIA H100 80GB HBM3","name":"H100 SXM","availability":"MEDIUM"}]}]}`)
	}))
	defer server.Close()

	dcs, err := newV2TestClient(t, server).ListDataCenters()
	if err != nil {
		t.Fatalf("ListDataCenters: %v", err)
	}
	want := DataCenter{ID: "AP-IN-1", Name: "AP-IN-1", Location: "ASIA", GpuAvailability: []GpuAvailabilityInDataCenter{
		{GpuTypeID: "NVIDIA H100 80GB HBM3", DisplayName: "H100 SXM", StockStatus: "Medium"}}}
	if len(dcs) != 1 || !reflect.DeepEqual(dcs[0], want) {
		t.Fatalf("data centers = %+v, want %+v", dcs, want)
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
