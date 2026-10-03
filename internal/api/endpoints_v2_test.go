package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// v2EndpointStub is a fake rest v2 control plane for the serverless reads and
// writes: /serverless, /serverless/{id}, /catalog/gpus (pool membership) and
// the worker listing.
type v2EndpointStub struct {
	endpoints map[string]string // id -> raw v2 json
	patched   map[string]interface{}
	renamed   map[string]interface{} // the v1 rename body
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
			if err := json.NewDecoder(r.Body).Decode(&s.renamed); err != nil {
				t.Errorf("decode rename body: %v", err)
			}
			_, _ = w.Write([]byte(`{}`))
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
	client.baseURL = server.URL // v1, for renames only
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
	if ep.WorkersMin != 0 || ep.WorkersMax != 3 || ep.IdleTimeout != 7 {
		t.Errorf("workers = %d/%d idle %d", ep.WorkersMin, ep.WorkersMax, ep.IdleTimeout)
	}
	if ep.ScalerType != "REQUEST_COUNT" || ep.ScalerValue != 4 {
		t.Errorf("scaler = %s/%d", ep.ScalerType, ep.ScalerValue)
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
		t.Errorf("scaler = %s/%d", ep.ScalerType, ep.ScalerValue)
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

func TestUpdateEndpoint_FillsNestedBlocksFromCurrent(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint}}
	client := stub.client(t)

	zero, five := 0, 5
	if _, err := client.UpdateEndpoint("ep-1", &EndpointUpdateRequest{WorkersMax: &five, ScalerValue: &zero}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	workers, _ := stub.patched["workers"].(map[string]interface{})
	// max changes; min and idleTimeout keep their current values, so a v2
	// partial update of the nested block cannot reset them
	if workers["max"] != float64(5) || workers["min"] != float64(0) || workers["idleTimeout"] != float64(7) {
		t.Errorf("workers = %v", workers)
	}
	scaling, _ := stub.patched["scaling"].(map[string]interface{})
	if scaling["type"] != "REQUEST_COUNT" || scaling["requestCount"] != float64(0) {
		t.Errorf("scaling = %v", scaling)
	}
	if _, ok := scaling["queueDelay"]; ok {
		t.Errorf("request count scaling must not send queueDelay: %v", scaling)
	}
	if _, ok := stub.patched["flashboot"]; ok {
		t.Errorf("unset flashboot must be omitted: %v", stub.patched)
	}
}

func TestUpdateEndpoint_ScalerTypeSwitch(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint}}
	client := stub.client(t)

	if _, err := client.UpdateEndpoint("ep-1", &EndpointUpdateRequest{ScalerType: "queue_delay"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	scaling, _ := stub.patched["scaling"].(map[string]interface{})
	// the current value carries over into the new scaler
	if scaling["type"] != "QUEUE_DELAY" || scaling["queueDelay"] != float64(4) {
		t.Errorf("scaling = %v", scaling)
	}
	if _, ok := stub.patched["workers"]; ok {
		t.Errorf("workers must be omitted when no worker flag is set: %v", stub.patched)
	}
}

func TestUpdateEndpoint_FlashbootSkipsTheRead(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint}}
	client := stub.client(t)

	off := false
	if _, err := client.UpdateEndpoint("ep-1", &EndpointUpdateRequest{Flashboot: &off}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.patched["flashboot"] != "OFF" {
		t.Errorf("patch = %v", stub.patched)
	}
	for _, req := range stub.requests {
		if req == "GET /serverless/ep-1" {
			t.Errorf("a flashboot update needs no current read: %v", stub.requests)
		}
	}
}

// a v2 rename also renames the endpoint's template, shared or not, so the name
// goes over v1 and never into the v2 body.
func TestUpdateEndpoint_RenameGoesOverV1(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint}}
	client := stub.client(t)

	ep, err := client.UpdateEndpoint("ep-1", &EndpointUpdateRequest{Name: "renamed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.renamed["name"] != "renamed" || len(stub.renamed) != 1 {
		t.Errorf("v1 rename body = %v", stub.renamed)
	}
	if stub.patched != nil {
		t.Errorf("a rename alone must not patch v2: %v", stub.patched)
	}
	if ep.ID != "ep-1" {
		t.Errorf("expected the re-read endpoint, got %+v", ep)
	}

	stub.renamed, stub.requests = nil, nil
	five := 5
	if _, err := client.UpdateEndpoint("ep-1", &EndpointUpdateRequest{Name: "again", WorkersMax: &five}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := stub.patched["name"]; ok {
		t.Errorf("name must never reach the v2 body: %v", stub.patched)
	}
	if stub.renamed["name"] != "again" {
		t.Errorf("v1 rename body = %v", stub.renamed)
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

// queue delay is fractional in v2 (minimum 0.5): it must parse, and a worker
// change must send the current value back unrounded.
func TestUpdateEndpoint_FractionalQueueDelaySurvives(t *testing.T) {
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": `{"id":"ep-1","name":"x",
		"scaling":{"type":"QUEUE_DELAY","queueDelay":0.5},"workers":{"min":0,"max":1,"idleTimeout":5}}`}}
	client := stub.client(t)

	ep, err := client.GetEndpoint("ep-1", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.ScalerType != "QUEUE_DELAY" {
		t.Errorf("scalerType = %q", ep.ScalerType)
	}

	if _, err := client.UpdateEndpoint("ep-1", &EndpointUpdateRequest{ScalerType: "QUEUE_DELAY"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	scaling, _ := stub.patched["scaling"].(map[string]interface{})
	if scaling["queueDelay"] != 0.5 {
		t.Errorf("queueDelay = %v, want 0.5 kept", scaling["queueDelay"])
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
