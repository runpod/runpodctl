package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// v2EndpointStub is a fake control plane for the serverless reads and writes:
// rest v2 /serverless, /serverless/{id}, /catalog/gpus (pool membership) and
// the worker listing, plus rest v1 /endpoints for updates and idle timeouts.
type v2EndpointStub struct {
	endpoints map[string]string // id -> raw v2 json
	patched   map[string]interface{}
	v1Patched map[string]interface{} // the v1 update body
	v1Idle    map[string]int         // id -> idleTimeout, as v1 reports it
	v1Status  int                    // non-zero: v1's single-endpoint read fails with it
	requests  []string
}

func (s *v2EndpointStub) client(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests = append(s.requests, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/catalog/gpus":
			_, _ = w.Write([]byte(`{"gpus":[
				{"id":"NVIDIA A40","pool":"AMPERE_48"},
				{"id":"NVIDIA RTX A6000","pool":"AMPERE_48"},
				{"id":"NVIDIA L4","pool":"ADA_24"}
			]}`))
		case r.URL.Path == "/serverless" && r.Method == http.MethodGet:
			var items []string
			for _, id := range []string{"ep-1", "ep-2"} {
				if raw, ok := s.endpoints[id]; ok {
					items = append(items, raw)
				}
			}
			_, _ = w.Write([]byte(`{"endpoints":[` + strings.Join(items, ",") + `],"pagination":{"hasNextPage":false}}`))
		case strings.HasPrefix(r.URL.Path, "/endpoints/") && r.Method == http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&s.v1Patched); err != nil {
				t.Errorf("decode v1 update body: %v", err)
			}
			_, _ = w.Write([]byte(`{}`))
		case r.URL.Path == "/endpoints" && r.Method == http.MethodGet:
			var items []map[string]interface{}
			for id, idle := range s.v1Idle {
				items = append(items, map[string]interface{}{"id": id, "idleTimeout": idle})
			}
			_ = json.NewEncoder(w).Encode(items)
		case strings.HasPrefix(r.URL.Path, "/endpoints/") && r.Method == http.MethodGet:
			id := strings.TrimPrefix(r.URL.Path, "/endpoints/")
			if s.v1Status != 0 {
				w.WriteHeader(s.v1Status)
				_, _ = w.Write([]byte(`{"error":"endpoint not found"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "idleTimeout": s.v1Idle[id]})
		case strings.HasSuffix(r.URL.Path, "/workers"):
			_, _ = w.Write([]byte(`{"workers":[{"id":"w-1","status":"RUNNING"}]}`))
		case strings.HasPrefix(r.URL.Path, "/serverless/"):
			id := strings.TrimPrefix(r.URL.Path, "/serverless/")
			raw, ok := s.endpoints[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"endpoint not found"}`))
				return
			}
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(raw))
			case http.MethodPatch:
				if err := json.NewDecoder(r.Body).Decode(&s.patched); err != nil {
					t.Errorf("decode patch body: %v", err)
				}
				_, _ = w.Write([]byte(raw))
			case http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("RUNPOD_REST_V2_URL", server.URL)
	client := newV2TestClient(t, server)
	client.baseURL = server.URL // v1, for updates and idle timeouts
	return client
}

const v2GpuEndpoint = `{
	"id":"ep-1","name":"endpoint-1","image":"img:1","args":"","disk":20,
	"env":{"A":"1"},"ports":["8000/http"],"registry":"reg-1",
	"gpu":{"pools":["AMPERE_48"],"excludedTypes":["NVIDIA RTX A6000"],"count":2,"minCudaVersion":"12.4"},
	"networkVolumes":["vol-1","vol-2"],"flashboot":"FLASHBOOT","timeout":600000,
	"scaling":{"type":"REQUEST_COUNT","requestCount":4},
	"workers":{"min":0,"max":3,"idleTimeout":7}
}`

// cpu endpoints report cpu as a list of configs
const v2CpuEndpoint = `{
	"id":"ep-2","name":"endpoint-2","image":"img:2",
	"cpu":[{"id":"cpu5c","memory":2,"vcpuCount":1}],
	"networkVolumes":[],"flashboot":"OFF","timeout":0,
	"scaling":{"type":"QUEUE_DELAY","queueDelay":1},
	"workers":{"min":1,"max":2,"idleTimeout":60}
}`

func TestGetEndpoint_MapsV2Shape(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint}}
	client := stub.client(t)

	ep, err := client.GetEndpoint("ep-1", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.ID != "ep-1" || ep.Name != "endpoint-1" {
		t.Errorf("id/name = %q/%q", ep.ID, ep.Name)
	}
	if ep.WorkersMin != 0 || ep.WorkersMax != 3 || ep.IdleTimeout == nil || *ep.IdleTimeout != 7 {
		t.Errorf("workers = %d/%d idle %v", ep.WorkersMin, ep.WorkersMax, ep.IdleTimeout)
	}
	if ep.ScalerType != "REQUEST_COUNT" || ep.ScalerValue != 4 {
		t.Errorf("scaler = %s/%v", ep.ScalerType, ep.ScalerValue)
	}
	if ep.GpuCount != 2 || ep.MinCudaVersion != "12.4" || ep.ExecutionTimeoutMs != 600000 {
		t.Errorf("gpuCount %d cuda %q timeout %d", ep.GpuCount, ep.MinCudaVersion, ep.ExecutionTimeoutMs)
	}
	if ep.Flashboot == nil || !*ep.Flashboot {
		t.Errorf("flashboot = %v, want true", ep.Flashboot)
	}
	// the pool expands to its types, minus the exclusion
	if len(ep.GpuTypeIDs) != 1 || ep.GpuTypeIDs[0] != "NVIDIA A40" {
		t.Errorf("gpuTypeIds = %v, want [NVIDIA A40]", ep.GpuTypeIDs)
	}
	if ep.NetworkVolumeID != "vol-1" || len(ep.NetworkVolumeIDs) != 2 || ep.NetworkVolumeIDs[1].NetworkVolumeID != "vol-2" {
		t.Errorf("network volumes = %q %+v", ep.NetworkVolumeID, ep.NetworkVolumeIDs)
	}
	if ep.URLs == nil || ep.URLs.Run != "https://api.runpod.ai/v2/ep-1/run" {
		t.Errorf("expected populated invoke urls, got %+v", ep.URLs)
	}
	if ep.Template != nil || ep.Workers != nil {
		t.Errorf("expansions must be off by default: template %v workers %v", ep.Template, ep.Workers)
	}
}

func TestGetEndpoint_CPUEndpoint(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-2": v2CpuEndpoint}}
	client := stub.client(t)

	ep, err := client.GetEndpoint("ep-2", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.Flashboot == nil || *ep.Flashboot {
		t.Errorf("flashboot = %v, want false for OFF", ep.Flashboot)
	}
	if ep.ScalerType != "QUEUE_DELAY" || ep.ScalerValue != 1 {
		t.Errorf("scaler = %s/%v", ep.ScalerType, ep.ScalerValue)
	}
	if len(ep.GpuTypeIDs) != 0 || ep.GpuCount != 0 {
		t.Errorf("cpu endpoint reports gpus: %v x%d", ep.GpuTypeIDs, ep.GpuCount)
	}
}

func TestGetEndpoint_Expansions(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint}}
	client := stub.client(t)

	ep, err := client.GetEndpoint("ep-1", true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.Template["imageName"] != "img:1" || ep.Template["containerDiskInGb"] != 20 || ep.Template["ports"] != "8000/http" {
		t.Errorf("inline template = %v", ep.Template)
	}
	if ep.Template["containerRegistryAuthId"] != "reg-1" {
		t.Errorf("registry = %v", ep.Template["containerRegistryAuthId"])
	}
	if len(ep.Workers) != 1 {
		t.Errorf("workers = %v, want the v2 worker listing", ep.Workers)
	}
}

func TestGetEndpoint_NotFound(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{}}
	client := stub.client(t)

	_, err := client.GetEndpoint("missing", false, false)
	if err == nil || !strings.Contains(err.Error(), "endpoint not found") {
		t.Fatalf("expected a not found error, got %v", err)
	}
}

func TestListEndpoints_MapsEveryEndpoint(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint, "ep-2": v2CpuEndpoint}}
	client := stub.client(t)

	eps, err := client.ListEndpoints(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(eps))
	}
	for _, ep := range eps {
		if ep.URLs == nil || ep.URLs.RunSync != "https://api.runpod.ai/v2/"+ep.ID+"/runsync" {
			t.Errorf("endpoint %s missing invoke urls: %+v", ep.ID, ep.URLs)
		}
	}
	if eps[0].WorkersMax != 3 || eps[1].WorkersMin != 1 {
		t.Errorf("workers not mapped: %+v", eps)
	}
}

func TestDeleteEndpoint_V2(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint}}
	client := stub.client(t)

	if err := client.DeleteEndpoint("ep-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stub.requests) != 1 || stub.requests[0] != "DELETE /serverless/ep-1" {
		t.Errorf("requests = %v", stub.requests)
	}
}

// updateModelsStub stands in for the two graphql calls UpdateEndpointModels
// makes: the config read and the saveEndpoint write. Nothing goes to rest:
// saveEndpoint needs templateId, which rest v2 does not report.
type updateModelsStub struct {
	config    string // the graphql endpoint object
	configErr string

	saveInput  map[string]interface{}
	saveCalled bool
}

func (s *updateModelsStub) client(t *testing.T) *Client {
	t.Helper()

	oldAPIURL := viper.GetString("apiUrl")
	t.Cleanup(func() { viper.Set("apiUrl", oldAPIURL) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			return
		}
		var body struct {
			Query     string                 `json:"query"`
			Variables map[string]interface{} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode gql body: %v", err)
			return
		}
		switch {
		case strings.Contains(body.Query, "EndpointConfig"):
			if s.configErr != "" {
				_, _ = w.Write([]byte(`{"errors":[{"message":"` + s.configErr + `"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"myself":{"endpoint":` + s.config + `}}}`))
		case strings.Contains(body.Query, "saveEndpoint"):
			s.saveCalled = true
			s.saveInput, _ = body.Variables["input"].(map[string]interface{})
			_, _ = w.Write([]byte(`{"data":{"saveEndpoint":{"id":"ep-abc","name":"my-ep"}}}`))
		default:
			t.Errorf("unexpected graphql query: %s", body.Query)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("RUNPOD_API_KEY", "test-key")
	viper.Set("apiUrl", server.URL)

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.baseURL = "http://v1.invalid"
	client.v2BaseURL = "http://v2.invalid"
	return client
}

func TestUpdateEndpointModels_RoundTripsConfig(t *testing.T) {
	stub := &updateModelsStub{
		config: `{
			"id": "ep-abc", "name": "my-ep", "templateId": "tpl-1",
			"gpuIds": "AMPERE_48,-NVIDIA RTX A6000", "gpuCount": 2, "instanceIds": null,
			"workersMin": 1, "workersMax": 5, "idleTimeout": 42,
			"scalerType": "REQUEST_COUNT", "scalerValue": 9, "locations": "US-GA-1",
			"networkVolumeId": "vol-9", "networkVolumeIds": [{"networkVolumeId": "vol-9"}],
			"executionTimeoutMs": 600000, "minCudaVersion": "12.4", "flashBootType": "FLASHBOOT"
		}`,
	}
	client := stub.client(t)

	if _, err := client.UpdateEndpointModels("ep-abc", []string{"https://huggingface.co/org/model:main"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := map[string]interface{}{
		"id":                 "ep-abc",
		"name":               "my-ep",
		"templateId":         "tpl-1",
		"gpuIds":             "AMPERE_48,-NVIDIA RTX A6000", // exclusions verbatim
		"gpuCount":           float64(2),
		"workersMin":         float64(1),
		"workersMax":         float64(5),
		"idleTimeout":        float64(42),
		"scalerType":         "REQUEST_COUNT",
		"scalerValue":        float64(9),
		"locations":          "US-GA-1",
		"networkVolumeId":    "vol-9",
		"executionTimeoutMs": float64(600000),
		"minCudaVersion":     "12.4",
		"flashBootType":      "FLASHBOOT",
	}
	for field, want := range checks {
		if got := stub.saveInput[field]; got != want {
			t.Errorf("input.%s = %v, want %v", field, got, want)
		}
	}
	refs, _ := stub.saveInput["modelReferences"].([]interface{})
	if len(refs) != 1 || refs[0] != "https://huggingface.co/org/model:main" {
		t.Errorf("unexpected modelReferences: %v", stub.saveInput["modelReferences"])
	}
	nvids, _ := stub.saveInput["networkVolumeIds"].([]interface{})
	if len(nvids) != 1 {
		t.Fatalf("expected 1 networkVolumeId, got %v", stub.saveInput["networkVolumeIds"])
	}
	if nvobj, _ := nvids[0].(map[string]interface{}); nvobj["networkVolumeId"] != "vol-9" {
		t.Errorf("expected networkVolumeId vol-9, got %v", nvobj)
	}
}

// saveEndpoint rejects "" for its FlashBootType enum
func TestUpdateEndpointModels_DefaultsFlashBootType(t *testing.T) {
	stub := &updateModelsStub{config: `{"id":"ep-abc","name":"my-ep","templateId":"tpl-1","gpuIds":"ADA_24","flashBootType":null}`}
	client := stub.client(t)

	if _, err := client.UpdateEndpointModels("ep-abc", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stub.saveInput["flashBootType"]; got != "OFF" {
		t.Errorf("flashBootType = %v, want OFF", got)
	}
	if refs, ok := stub.saveInput["modelReferences"].([]interface{}); !ok || len(refs) != 0 {
		t.Errorf("nil refs must clear as [], got %v", stub.saveInput["modelReferences"])
	}
}

func TestUpdateEndpointModels_ConfigReadFailureSkipsWrite(t *testing.T) {
	stub := &updateModelsStub{configErr: "Something went wrong."}
	client := stub.client(t)

	_, err := client.UpdateEndpointModels("ep-abc", nil)
	if err == nil || !strings.Contains(err.Error(), "failed to fetch endpoint") {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.saveCalled {
		t.Error("saveEndpoint must not be called when the config read fails")
	}
}

// a model-only update of an endpoint that doesn't exist is not_found, as it
// was when the first read went through GetEndpoint
func TestUpdateEndpointModels_MissingEndpointIsNotFound(t *testing.T) {
	stub := &updateModelsStub{config: "null"}
	_, err := stub.client(t).UpdateEndpointModels("ep-typo", nil)
	var coded interface{ ErrorCode() string }
	if !errors.As(err, &coded) || coded.ErrorCode() != "not_found" {
		t.Fatalf("err = %v, want not_found", err)
	}
	if stub.saveCalled {
		t.Error("saveEndpoint must not be called for a missing endpoint")
	}
}

// saveEndpoint rejects an empty gpuIds on anything without instanceIds, so
// report that rather than sending a write that drops the gpu selection.
func TestUpdateEndpointModels_EmptyGpuIDsOnGpuEndpointSkipsWrite(t *testing.T) {
	stub := &updateModelsStub{config: `{"id":"ep-abc","name":"my-ep","templateId":"tpl-1","gpuIds":null,"instanceIds":[]}`}
	client := stub.client(t)

	_, err := client.UpdateEndpointModels("ep-abc", nil)
	if err == nil || !strings.Contains(err.Error(), "no gpuIds and no instanceIds") {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.saveCalled {
		t.Error("saveEndpoint must not be called with an empty gpu selection")
	}
}

func TestUpdateEndpointModels_CPUEndpointNeedsNoGpuIDs(t *testing.T) {
	stub := &updateModelsStub{config: `{"id":"ep-abc","name":"my-ep","templateId":"tpl-1","gpuIds":null,"instanceIds":["cpu5c-1-2"]}`}
	client := stub.client(t)

	if _, err := client.UpdateEndpointModels("ep-abc", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids, _ := stub.saveInput["instanceIds"].([]interface{})
	if len(ids) != 1 || ids[0] != "cpu5c-1-2" {
		t.Errorf("instanceIds = %v, want the cpu instance preserved", stub.saveInput["instanceIds"])
	}
}

// a missing catalog must not print as an endpoint with no gpus
func TestGetEndpoint_CatalogFailureIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog/gpus" {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"title":"Service Unavailable","status":503}`))
			return
		}
		_, _ = w.Write([]byte(v2GpuEndpoint))
	}))
	defer server.Close()
	client := newV2TestClient(t, server)

	if _, err := client.GetEndpoint("ep-1", false, false); err == nil || !strings.Contains(err.Error(), "gpu catalog") {
		t.Fatalf("expected a catalog error, got %v", err)
	}
}

// without the worker read, an endpoint would print as one with no workers
func TestGetEndpoint_WorkerReadFailureIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/workers"):
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/catalog/gpus":
			_, _ = w.Write([]byte(`{"gpus":[]}`))
		default:
			_, _ = w.Write([]byte(v2GpuEndpoint))
		}
	}))
	defer server.Close()

	_, err := newV2TestClient(t, server).GetEndpoint("ep-1", false, true)
	if err == nil || !strings.Contains(err.Error(), "failed to read workers") {
		t.Fatalf("expected a worker read error, got %v", err)
	}
}

func TestListEndpoints_IncludeWorkers(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint, "ep-2": v2CpuEndpoint}}
	client := stub.client(t)

	eps, err := client.ListEndpoints(&EndpointListOptions{IncludeWorkers: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, ep := range eps {
		if len(ep.Workers) != 1 {
			t.Errorf("%s workers = %v", ep.ID, ep.Workers)
		}
	}
}

// every v2 patch of an endpoint renames its template, so an update is one v1
// patch, in v1's flat shape, and never touches v2.
func TestUpdateEndpoint_OneV1Patch(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint}}
	client := stub.client(t)

	five, thirty := 5, 30
	req := &EndpointUpdateRequest{Name: "renamed", WorkersMax: &five, IdleTimeout: &thirty, ScalerType: "QUEUE_DELAY"}
	if err := client.UpdateEndpoint("ep-1", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]interface{}{"name": "renamed", "workersMax": float64(5), "idleTimeout": float64(30), "scalerType": "QUEUE_DELAY"}
	if len(stub.v1Patched) != len(want) {
		t.Errorf("v1 body = %v, want exactly %v", stub.v1Patched, want)
	}
	for k, v := range want {
		if stub.v1Patched[k] != v {
			t.Errorf("v1 body %s = %v, want %v", k, stub.v1Patched[k], v)
		}
	}
	if stub.patched != nil || len(stub.requests) != 1 {
		t.Errorf("an update must be a single v1 request, got %v (v2 body %v)", stub.requests, stub.patched)
	}
}

// v2 leaves workers.idleTimeout out on request-count endpoints; get and list
// read it from v1 rather than print 0.
func TestEndpointReads_FillIdleTimeoutFromV1(t *testing.T) {
	noIdle := strings.Replace(v2GpuEndpoint, `"workers":{"min":0,"max":3,"idleTimeout":7}`, `"workers":{"min":0,"max":3}`, 1)
	if noIdle == v2GpuEndpoint {
		t.Fatal("fixture did not change")
	}
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": noIdle, "ep-2": v2CpuEndpoint}, v1Idle: map[string]int{"ep-1": 50, "ep-2": 999}}
	client := stub.client(t)

	ep, err := client.GetEndpoint("ep-1", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.IdleTimeout == nil || *ep.IdleTimeout != 50 {
		t.Errorf("get idleTimeout = %v, want 50 from v1", ep.IdleTimeout)
	}

	eps, err := client.ListEndpoints(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := map[string]int{}
	for _, e := range eps {
		if e.IdleTimeout != nil {
			got[e.ID] = *e.IdleTimeout
		}
	}
	// ep-2's v2 value stands; only the missing one comes from v1
	if got["ep-1"] != 50 || got["ep-2"] != 60 {
		t.Errorf("list idle timeouts = %v, want ep-1 50 (v1) and ep-2 60 (v2)", got)
	}

	stub.requests = nil
	if _, err := client.GetEndpoint("ep-2", false, false); err != nil {
		t.Fatal(err)
	}
	for _, r := range stub.requests {
		if strings.HasPrefix(r, "GET /endpoints") {
			t.Errorf("an endpoint v2 reports in full must not read v1: %v", stub.requests)
		}
	}
}

// a fractional queue delay prints as itself, and a pool the catalog doesn't
// list prints as its id rather than leaving the endpoint with no gpus.
func TestGetEndpoint_FractionalDelayAndUnknownPool(t *testing.T) {
	raw := strings.NewReplacer(
		`"scaling":{"type":"REQUEST_COUNT","requestCount":4}`, `"scaling":{"type":"QUEUE_DELAY","queueDelay":0.5}`,
		`"pools":["AMPERE_48"]`, `"pools":["ada_24","RETIRED_80"]`,
	).Replace(v2GpuEndpoint)
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": raw}}
	ep, err := stub.client(t).GetEndpoint("ep-1", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.ScalerType != "QUEUE_DELAY" || ep.ScalerValue != 0.5 {
		t.Errorf("scaler = %s/%v, want QUEUE_DELAY/0.5", ep.ScalerType, ep.ScalerValue)
	}
	// ada_24 matches the catalog's ADA_24 case-insensitively
	want := []string{"NVIDIA L4", "RETIRED_80"}
	if strings.Join(ep.GpuTypeIDs, ",") != strings.Join(want, ",") {
		t.Errorf("gpuTypeIds = %v, want %v", ep.GpuTypeIDs, want)
	}
}

// an endpoint v1's list doesn't report (created or deleted between the two
// reads) gets no idleTimeout, never 0, and doesn't fail the whole list
func TestListEndpoints_MissingV1IdleTimeoutIsLeftOut(t *testing.T) {
	noIdle := strings.Replace(v2GpuEndpoint, `"workers":{"min":0,"max":3,"idleTimeout":7}`, `"workers":{"min":0,"max":3}`, 1)
	noIdleCPU := strings.Replace(v2CpuEndpoint, `"workers":{"min":1,"max":2,"idleTimeout":60}`, `"workers":{"min":1,"max":2}`, 1)
	// two endpoints miss it, so the fill reads v1's list, which lacks ep-1
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": noIdle, "ep-2": noIdleCPU}, v1Idle: map[string]int{"ep-2": 5}}
	eps, err := stub.client(t).ListEndpoints(nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, e := range eps {
		switch {
		case e.ID == "ep-1" && e.IdleTimeout != nil:
			t.Errorf("ep-1 idleTimeout = %d, want none", *e.IdleTimeout)
		case e.ID == "ep-2" && (e.IdleTimeout == nil || *e.IdleTimeout != 5):
			t.Errorf("ep-2 idleTimeout = %v, want 5 from v1", e.IdleTimeout)
		}
	}
}

// a failed v1 read fails the get, but never as not_found: v2 just returned
// the endpoint
func TestGetEndpoint_V1IdleTimeoutFailureIsNotNotFound(t *testing.T) {
	noIdle := strings.Replace(v2GpuEndpoint, `"workers":{"min":0,"max":3,"idleTimeout":7}`, `"workers":{"min":0,"max":3}`, 1)
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": noIdle}, v1Status: http.StatusNotFound}
	_, err := stub.client(t).GetEndpoint("ep-1", false, false)
	if err == nil || !strings.Contains(err.Error(), "failed to read the idle timeout") {
		t.Fatalf("err = %v, want an idle timeout read error", err)
	}
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) && coded.ErrorCode() == "not_found" {
		t.Fatalf("err code = not_found; the endpoint exists")
	}
}

// a pool whose every type is excluded prints as its pool id, not as no gpus
func TestGetEndpoint_AllExcludedPoolKeepsPoolID(t *testing.T) {
	raw := strings.Replace(v2GpuEndpoint, `"pools":["AMPERE_48"],"excludedTypes":["NVIDIA RTX A6000"]`, `"pools":["ADA_24"],"excludedTypes":["NVIDIA L4"]`, 1)
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": raw}}
	ep, err := stub.client(t).GetEndpoint("ep-1", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ep.GpuTypeIDs, ",") != "ADA_24" {
		t.Fatalf("gpuTypeIds = %v, want [ADA_24]", ep.GpuTypeIDs)
	}
}

// placement and a cmd-based start command are part of what the endpoint runs.
func TestGetEndpoint_LocationsAndStartCmd(t *testing.T) {
	raw := strings.Replace(v2CpuEndpoint, `"networkVolumes":[]`, `"networkVolumes":[],"dataCenterIds":["EU-RO-1","US-MO-1"],"cmd":["python","-u","handler.py"]`, 1)
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-2": raw}}
	ep, err := stub.client(t).GetEndpoint("ep-2", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.Locations != "EU-RO-1,US-MO-1" {
		t.Errorf("locations = %q", ep.Locations)
	}
	cmd, _ := ep.Template["dockerStartCmd"].([]string)
	if strings.Join(cmd, " ") != "python -u handler.py" {
		t.Errorf("template dockerStartCmd = %#v", ep.Template["dockerStartCmd"])
	}
}
