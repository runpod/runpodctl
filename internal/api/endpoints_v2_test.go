package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

type v2EndpointStub struct {
	endpoints map[string]string // id -> raw v2 json
	patched   map[string]interface{}
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
	client.baseURL = server.URL
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
	if err == nil || !strings.Contains(err.Error(), "failed to fetch endpoint") || !strings.Contains(err.Error(), stub.configErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	var gqlErr *GraphQLError
	if !errors.As(err, &gqlErr) || gqlErr.Message != stub.configErr {
		t.Fatalf("err = %v, want graphql error with message %q", err, stub.configErr)
	}
	if stub.saveCalled {
		t.Error("saveEndpoint must not be called when the config read fails")
	}
}

// a model-only update of an endpoint that doesn't exist is not_found, as it
// was when the first read went through GetEndpoint
func TestUpdateEndpointModels_MissingEndpointIsNotFound(t *testing.T) {
	// a null endpoint, and the error graphql actually sends for an unknown id
	for _, stub := range []*updateModelsStub{{config: "null"}, {configErr: "Endpoint not found"}} {
		_, err := stub.client(t).UpdateEndpointModels("ep-typo", nil)
		var coded interface{ ErrorCode() string }
		if !errors.As(err, &coded) || coded.ErrorCode() != "not_found" {
			t.Fatalf("err = %v, want not_found", err)
		}
		if stub.saveCalled {
			t.Error("saveEndpoint must not be called for a missing endpoint")
		}
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

func TestUpdateEndpoint_V2Patch(t *testing.T) {
	zero, five, thirty := 0, 5, 30
	cases := []struct {
		name  string
		req   EndpointUpdateRequest
		want  string
		reads int
	}{
		{"name", EndpointUpdateRequest{Name: "renamed"}, `{"name":"renamed"}`, 0},
		{"workers", EndpointUpdateRequest{WorkersMin: &zero, WorkersMax: &five, IdleTimeout: &thirty}, `{"workers":{"min":0,"max":5,"idleTimeout":30}}`, 0},
		{"zero max", EndpointUpdateRequest{WorkersMax: &zero}, `{"workers":{"max":0}}`, 0},
		{"delay", EndpointUpdateRequest{ScalerType: "QUEUE_DELAY", ScalerValue: &five}, `{"scaling":{"type":"QUEUE_DELAY","queueDelay":5}}`, 0},
		{"requests", EndpointUpdateRequest{ScalerType: "REQUEST_COUNT", ScalerValue: &five}, `{"scaling":{"type":"REQUEST_COUNT","requestCount":5}}`, 0},
		{"threshold only", EndpointUpdateRequest{ScalerValue: &five}, `{"scaling":{"type":"REQUEST_COUNT","requestCount":5}}`, 1},
		{"type only", EndpointUpdateRequest{ScalerType: "QUEUE_DELAY"}, `{"scaling":{"type":"QUEUE_DELAY","queueDelay":4}}`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": v2GpuEndpoint}}
			if err := stub.client(t).UpdateEndpoint("ep-1", &tc.req); err != nil {
				t.Fatal(err)
			}
			var want map[string]any
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(stub.patched, want) {
				t.Fatalf("body = %#v, want %#v", stub.patched, want)
			}
			wantRequests := []string{"PATCH /serverless/ep-1"}
			if tc.reads == 1 {
				wantRequests = append([]string{"GET /serverless/ep-1"}, wantRequests...)
			}
			if !reflect.DeepEqual(stub.requests, wantRequests) {
				t.Fatalf("requests = %v, want %v", stub.requests, wantRequests)
			}
		})
	}
}

func TestEndpointReads_NullIdleTimeout(t *testing.T) {
	for _, idle := range []string{"", `,"idleTimeout":null`} {
		t.Run(idle, func(t *testing.T) {
			noIdle := strings.Replace(v2GpuEndpoint, `"workers":{"min":0,"max":3,"idleTimeout":7}`, `"workers":{"min":0,"max":3`+idle+`}`, 1)
			stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": noIdle, "ep-2": v2CpuEndpoint}}
			client := stub.client(t)
			ep, err := client.GetEndpoint("ep-1", false, false)
			if err != nil {
				t.Fatal(err)
			}
			if ep.IdleTimeout != nil {
				t.Fatalf("get idleTimeout = %v, want nil", ep.IdleTimeout)
			}
			data, err := json.Marshal(ep)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `"idleTimeout":null`) {
				t.Fatalf("expected explicit null: %s", data)
			}
			eps, err := client.ListEndpoints(nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(eps) != 2 || eps[0].IdleTimeout != nil || eps[1].IdleTimeout == nil || *eps[1].IdleTimeout != 60 {
				t.Fatalf("unexpected endpoints: %#v", eps)
			}
			for _, r := range stub.requests {
				if strings.Contains(r, "/endpoints") {
					t.Fatalf("unexpected v1 request: %s", r)
				}
			}
		})
	}
}

func TestUpdateEndpoint_PreservesFractionalDelay(t *testing.T) {
	raw := strings.Replace(v2GpuEndpoint, `"scaling":{"type":"REQUEST_COUNT","requestCount":4}`, `"scaling":{"type":"QUEUE_DELAY","queueDelay":0.5}`, 1)
	stub := &v2EndpointStub{endpoints: map[string]string{"ep-1": raw}}
	client := stub.client(t)
	if err := client.UpdateEndpoint("ep-1", &EndpointUpdateRequest{ScalerType: "QUEUE_DELAY"}); err != nil {
		t.Fatal(err)
	}
	scaling, ok := stub.patched["scaling"].(map[string]any)
	if !ok {
		t.Fatalf("missing scaling: %v", stub.patched)
	}
	if got := scaling["queueDelay"]; got != 0.5 {
		t.Fatalf("queueDelay = %v, want 0.5", got)
	}
	stub.patched = nil
	if err := client.UpdateEndpoint("ep-1", &EndpointUpdateRequest{ScalerType: "REQUEST_COUNT"}); err == nil || !strings.Contains(err.Error(), "--scale-threshold") {
		t.Fatalf("expected an explicit integer threshold requirement: %v", err)
	}
	if stub.patched != nil {
		t.Fatalf("invalid scaling must not write: %v", stub.patched)
	}
}

func TestUpdateEndpoint_V2Failures(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable} {
		for _, read := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/read=%t", status, read), func(t *testing.T) {
				var methods []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					methods = append(methods, r.Method)
					if r.URL.Path != "/serverless/ep-1" {
						t.Errorf("unexpected path %s", r.URL.Path)
					}
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(status)
					if _, err := fmt.Fprintf(w, `{"status":%d,"title":"request failed","detail":"test failure"}`, status); err != nil {
						t.Error(err)
					}
				}))
				t.Cleanup(server.Close)
				req := &EndpointUpdateRequest{Name: "renamed"}
				method := http.MethodPatch
				if read {
					req.ScalerType = "QUEUE_DELAY"
					method = http.MethodGet
				}
				err := newV2TestClient(t, server).UpdateEndpoint("ep-1", req)
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Status != status {
					t.Fatalf("error = %v, want status %d", err, status)
				}
				want := []string{method}
				if read && status == http.StatusServiceUnavailable {
					want = make([]string, maxRetryAttempts)
					for i := range want {
						want[i] = method
					}
				}
				if !reflect.DeepEqual(methods, want) {
					t.Fatalf("requests = %v, want %v", methods, want)
				}
			})
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
