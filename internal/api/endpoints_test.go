package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

func TestListEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/endpoints" {
			t.Errorf("expected /endpoints, got %s", r.URL.Path)
		}
		// serve the raw rest wire shape, not a re-encoded Endpoint struct, so
		// the test exercises the real api format (networkVolumeIds as bare id
		// strings) — a struct round-trip would emit the object shape instead
		// and hide read/write divergences.
		w.Write([]byte(`[
			{"id":"ep-1","name":"endpoint-1","networkVolumeIds":["vol-1"]},
			{"id":"ep-2","name":"endpoint-2"}
		]`))
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	endpoints, err := client.ListEndpoints(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(endpoints))
	}
	if len(endpoints[0].NetworkVolumeIDs) != 1 || endpoints[0].NetworkVolumeIDs[0].NetworkVolumeID != "vol-1" {
		t.Errorf("expected vol-1 on first endpoint, got %+v", endpoints[0].NetworkVolumeIDs)
	}
}

func TestEndpointNetworkVolumeIDsUnmarshal(t *testing.T) {
	// rest read shape: bare id strings.
	var strShape Endpoint
	if err := json.Unmarshal([]byte(`{"id":"ep-1","networkVolumeIds":["vol-1","vol-2"]}`), &strShape); err != nil {
		t.Fatalf("failed to unmarshal string shape: %v", err)
	}
	if len(strShape.NetworkVolumeIDs) != 2 || strShape.NetworkVolumeIDs[0].NetworkVolumeID != "vol-1" {
		t.Fatalf("unexpected string-shape parse: %+v", strShape.NetworkVolumeIDs)
	}

	// graphql write shape: objects.
	var objShape Endpoint
	if err := json.Unmarshal([]byte(`{"id":"ep-2","networkVolumeIds":[{"networkVolumeId":"vol-3","dataCenterId":"US-GA-1"}]}`), &objShape); err != nil {
		t.Fatalf("failed to unmarshal object shape: %v", err)
	}
	if len(objShape.NetworkVolumeIDs) != 1 || objShape.NetworkVolumeIDs[0].NetworkVolumeID != "vol-3" || objShape.NetworkVolumeIDs[0].DataCenterID != "US-GA-1" {
		t.Fatalf("unexpected object-shape parse: %+v", objShape.NetworkVolumeIDs)
	}
}

func TestGetEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/endpoints/ep-123" {
			t.Errorf("expected /endpoints/ep-123, got %s", r.URL.Path)
		}
		// raw rest wire shape, including a network volume returned as a bare id
		// string — this is what regressed serverless get in production.
		w.Write([]byte(`{
			"id":"ep-123",
			"name":"my-endpoint",
			"workersMin":0,
			"workersMax":3,
			"networkVolumeId":"vol-9",
			"networkVolumeIds":["vol-9"]
		}`))
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	endpoint, err := client.GetEndpoint("ep-123", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint.ID != "ep-123" {
		t.Errorf("expected ep-123, got %s", endpoint.ID)
	}
	if len(endpoint.NetworkVolumeIDs) != 1 || endpoint.NetworkVolumeIDs[0].NetworkVolumeID != "vol-9" {
		t.Errorf("expected vol-9, got %+v", endpoint.NetworkVolumeIDs)
	}
}

func TestEndpointCreateGQLInputZeroValues(t *testing.T) {
	zero := 0
	data, err := json.Marshal(EndpointCreateGQLInput{
		Name:               "ep",
		WorkersMin:         &zero,
		WorkersMax:         &zero,
		ExecutionTimeoutMs: &zero,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"workersMin", "workersMax", "executionTimeoutMs"} {
		got, ok := out[field]
		if !ok {
			t.Errorf("expected %s in create input, got %s", field, data)
			continue
		}
		if got != float64(0) {
			t.Errorf("expected %s 0, got %#v", field, got)
		}
	}

	// unset must stay omitted, or create would clobber server defaults.
	bare, err := json.Marshal(EndpointCreateGQLInput{Name: "ep"})
	if err != nil {
		t.Fatalf("marshal bare: %v", err)
	}
	for _, field := range []string{"workersMin", "workersMax", "executionTimeoutMs", "gpuCount"} {
		if strings.Contains(string(bare), field) {
			t.Errorf("expected %s to be omitted when unset, got %s", field, bare)
		}
	}
}

func TestEndpointCreateGQLInputSerialization(t *testing.T) {
	data, err := json.Marshal(EndpointCreateGQLInput{
		Name:             "ep",
		TemplateID:       "tpl-123",
		InstanceIDs:      []string{"cpu3g-4-16"},
		NetworkVolumeIDs: []NetworkVolumeIDInput{{NetworkVolumeID: "vol-1"}},
		FlashBootType:    "OFF",
	})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	s := string(data)
	// saveEndpoint requires name (String!), so it must always be present.
	if !strings.Contains(s, `"name":"ep"`) {
		t.Fatalf("expected name to be present, got %s", s)
	}
	if !strings.Contains(s, `"instanceIds":["cpu3g-4-16"]`) {
		t.Fatalf("expected instanceIds, got %s", s)
	}
	// multi-region volumes serialize as an array of {networkVolumeId} objects.
	if !strings.Contains(s, `"networkVolumeIds":[{"networkVolumeId":"vol-1"}]`) {
		t.Fatalf("expected networkVolumeIds objects, got %s", s)
	}
	if !strings.Contains(s, `"flashBootType":"OFF"`) {
		t.Fatalf("expected flashBootType, got %s", s)
	}
}

func TestCreateEndpointGQLIncludesModelReferences(t *testing.T) {
	modelReference := "https://local/user/model:hash"
	oldAPIURL := viper.GetString("apiUrl")
	t.Cleanup(func() {
		viper.Set("apiUrl", oldAPIURL)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var body struct {
			Query     string `json:"query"`
			Variables struct {
				Input EndpointCreateGQLInput `json:"input"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if !strings.Contains(body.Query, "modelReferences") {
			t.Error("expected query to select modelReferences")
		}
		if !strings.Contains(body.Query, "templateId") {
			t.Error("expected query to select templateId")
		}
		if len(body.Variables.Input.ModelReferences) != 1 || body.Variables.Input.ModelReferences[0] != modelReference {
			t.Fatalf("unexpected model references: %#v", body.Variables.Input.ModelReferences)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"saveEndpoint": map[string]interface{}{
					"id":              "new-ep-id",
					"name":            body.Variables.Input.Name,
					"templateId":      body.Variables.Input.TemplateID,
					"modelReferences": body.Variables.Input.ModelReferences,
				},
			},
		})
	}))
	defer server.Close()

	viper.Set("apiUrl", server.URL)
	client := &Client{
		baseURL:    "http://rest.example",
		apiKey:     "test-key",
		httpClient: server.Client(),
	}

	endpoint, err := client.CreateEndpointGQL(&EndpointCreateGQLInput{
		Name:            "test-endpoint",
		TemplateID:      "tpl-123",
		ModelReferences: []string{modelReference},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint.TemplateID != "tpl-123" {
		t.Errorf("expected template id tpl-123, got %s", endpoint.TemplateID)
	}
	if len(endpoint.ModelReferences) != 1 || endpoint.ModelReferences[0] != modelReference {
		t.Fatalf("unexpected response model references: %#v", endpoint.ModelReferences)
	}
}

func TestEndpointZeroNumericFieldsSurviveOutput(t *testing.T) {
	render := func(t *testing.T, body string) map[string]interface{} {
		t.Helper()
		var e Endpoint
		if err := json.Unmarshal([]byte(body), &e); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out map[string]interface{}
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		return out
	}

	// all zero on purpose: a presence check only detects omitempty at 0.
	allZero := render(t, `{"id":"ep-123","name":"my-endpoint","workersMin":0,"workersMax":0,
		"idleTimeout":0,"scalerValue":0,"gpuCount":0,"executionTimeoutMs":0}`)
	for _, field := range []string{"workersMin", "workersMax", "idleTimeout", "scalerValue", "gpuCount", "executionTimeoutMs"} {
		got, ok := allZero[field]
		if !ok {
			t.Errorf("expected %s in rendered output, got %#v", field, allZero)
			continue
		}
		if got != float64(0) {
			t.Errorf("expected %s 0, got %#v", field, got)
		}
	}

	nonZero := render(t, `{"id":"ep-123","name":"my-endpoint","workersMin":2,"workersMax":3,"idleTimeout":5}`)
	if nonZero["workersMin"] != float64(2) {
		t.Errorf("expected workersMin 2, got %#v", nonZero["workersMin"])
	}
	if nonZero["workersMax"] != float64(3) {
		t.Errorf("expected workersMax 3, got %#v", nonZero["workersMax"])
	}
	if nonZero["idleTimeout"] != float64(5) {
		t.Errorf("expected idleTimeout 5, got %#v", nonZero["idleTimeout"])
	}
}

func TestUpdateEndpoint(t *testing.T) {
	wrkMin := 0
	wrkMax := 5

	var body map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/endpoints/ep-123" {
			t.Errorf("expected /endpoints/ep-123, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		// raw wire shape so this does not depend on Endpoint's tags.
		w.Write([]byte(`{"id":"ep-123","workersMin":0,"workersMax":5}`))
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	endpoint, err := client.UpdateEndpoint("ep-123", &EndpointUpdateRequest{
		WorkersMin: &wrkMin,
		WorkersMax: &wrkMax,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, ok := body["workersMin"]; !ok {
		t.Errorf("expected workersMin in patch body, got %#v", body)
	} else if got != float64(0) {
		t.Errorf("expected workersMin 0, got %#v", got)
	}
	if got := body["workersMax"]; got != float64(5) {
		t.Errorf("expected workersMax 5, got %#v", got)
	}
	// unset fields must still be omitted rather than sent as zeroes.
	if _, ok := body["idleTimeout"]; ok {
		t.Errorf("expected idleTimeout to be omitted, got %#v", body)
	}
	if _, ok := body["scalerValue"]; ok {
		t.Errorf("expected scalerValue to be omitted, got %#v", body)
	}

	if endpoint.ID != "ep-123" {
		t.Errorf("expected ep-123, got %q", endpoint.ID)
	}
	if endpoint.WorkersMax != 5 {
		t.Errorf("expected 5, got %d", endpoint.WorkersMax)
	}
}

func TestUpdateEndpointTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		vars, _ := body["variables"].(map[string]interface{})
		input, _ := vars["input"].(map[string]interface{})
		if input["endpointId"] != "ep-123" {
			t.Fatalf("expected endpoint id ep-123, got %#v", input["endpointId"])
		}
		if input["templateId"] != "tpl-456" {
			t.Fatalf("expected template id tpl-456, got %#v", input["templateId"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"updateEndpointTemplate": map[string]interface{}{
					"id":         "ep-123",
					"templateId": "tpl-456",
				},
			},
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	viper.Set("apiUrl", server.URL)
	t.Cleanup(func() {
		viper.Set("apiUrl", "")
	})

	client, _ := NewClient()

	if err := client.UpdateEndpointTemplate("ep-123", "tpl-456"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateEndpointTemplate_GraphQLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{
				{"message": "template not found"},
			},
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	viper.Set("apiUrl", server.URL)
	t.Cleanup(func() {
		viper.Set("apiUrl", "")
	})

	client, _ := NewClient()

	err := client.UpdateEndpointTemplate("ep-123", "tpl-456")
	if err == nil {
		t.Fatal("expected graphql error")
	}
	if err.Error() != "graphql error: template not found" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// updateModelsStub stands in for the three calls UpdateEndpointModels makes:
// the REST endpoint read, the GraphQL gpuIds read, and the saveEndpoint write.
// restJSON is the REST body (which, like prod, never carries gpuIds); gpuIDs is
// what the GraphQL read reports; gpuIDsErr makes that read fail instead.
type updateModelsStub struct {
	restJSON  string
	gpuIDs    string
	gpuIDsErr string

	saveInput  map[string]interface{}
	saveCalled bool
}

// client wires a client to a server backed by the stub, restoring the global
// viper key it has to set on the way out.
func (s *updateModelsStub) client(t *testing.T) *Client {
	t.Helper()

	oldAPIURL := viper.GetString("apiUrl")
	t.Cleanup(func() { viper.Set("apiUrl", oldAPIURL) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/endpoints/"):
			_, _ = w.Write([]byte(s.restJSON))
		case r.Method == http.MethodPost:
			var body struct {
				Query     string                 `json:"query"`
				Variables map[string]interface{} `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode gql body: %v", err)
				return
			}
			switch {
			case strings.Contains(body.Query, "EndpointGpuIDs"):
				if s.gpuIDsErr != "" {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"errors": []map[string]interface{}{{"message": s.gpuIDsErr}},
					})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"myself": map[string]interface{}{
							"endpoint": map[string]interface{}{"gpuIds": s.gpuIDs},
						},
					},
				})
			case strings.Contains(body.Query, "saveEndpoint"):
				s.saveCalled = true
				s.saveInput, _ = body.Variables["input"].(map[string]interface{})
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"saveEndpoint": map[string]interface{}{"id": "ep-abc", "name": "my-ep"},
					},
				})
			default:
				// nothing may reconstruct gpuIds from the REST gpuTypeIds — notably
				// no serverlessGpuPools lookup: that translation cannot express the
				// pool exclusions gpuIds carries.
				t.Errorf("unexpected graphql query: %s", body.Query)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("RUNPOD_API_KEY", "test-key")
	viper.Set("apiUrl", server.URL)

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.baseURL = server.URL
	return client
}

func TestUpdateEndpointModels_RoundTripsConfig(t *testing.T) {
	stub := &updateModelsStub{
		// REST read: real wire shape, with a bare-string networkVolumeId.
		restJSON: `{
			"id": "ep-abc",
			"name": "my-ep",
			"templateId": "tpl-1",
			"gpuTypeIds": ["NVIDIA L4"],
			"workersMin": 1,
			"workersMax": 5,
			"idleTimeout": 42,
			"scalerType": "REQUEST_COUNT",
			"scalerValue": 9,
			"networkVolumeId": "vol-9",
			"networkVolumeIds": ["vol-9"]
		}`,
		gpuIDs: "ADA_24",
	}
	client := stub.client(t)

	_, err := client.UpdateEndpointModels("ep-abc", []string{"https://huggingface.co/org/model:main"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All existing config fields must be present in the mutation input.
	checks := map[string]interface{}{
		"id":          "ep-abc",
		"name":        "my-ep",
		"templateId":  "tpl-1",
		"gpuIds":      "ADA_24",
		"workersMax":  float64(5),
		"idleTimeout": float64(42),
		"scalerType":  "REQUEST_COUNT",
		"scalerValue": float64(9),
	}
	for field, want := range checks {
		if got := stub.saveInput[field]; got != want {
			t.Errorf("input.%s = %v, want %v", field, got, want)
		}
	}

	// modelReferences must carry the new value, not the old one.
	refs, _ := stub.saveInput["modelReferences"].([]interface{})
	if len(refs) != 1 || refs[0] != "https://huggingface.co/org/model:main" {
		t.Errorf("unexpected modelReferences: %v", stub.saveInput["modelReferences"])
	}

	// networkVolumeIds must be passed as objects, not bare strings.
	nvids, _ := stub.saveInput["networkVolumeIds"].([]interface{})
	if len(nvids) != 1 {
		t.Fatalf("expected 1 networkVolumeId, got %v", stub.saveInput["networkVolumeIds"])
	}
	nvobj, _ := nvids[0].(map[string]interface{})
	if nvobj["networkVolumeId"] != "vol-9" {
		t.Errorf("expected networkVolumeId vol-9, got %v", nvobj)
	}

	// the REST fixture above has neither "flashboot" nor "flashBootType", so
	// this must fall back to the "OFF" default rather than sending "".
	if got := stub.saveInput["flashBootType"]; got != "OFF" {
		t.Errorf("expected flashBootType OFF when REST returns no flash boot info, got %v", got)
	}
}

// TestUpdateEndpointModels_DerivesFlashBootTypeFromRESTBool covers a bug found
// live (STO-360 e2e test): REST GET /v1/endpoints/{id} returns a "flashboot"
// bool, never the "flashBootType" enum string saveEndpoint requires, so
// endpoint.FlashBootType was always "" after GetEndpoint and saveEndpoint
// rejected it with `Value "" does not exist in "FlashBootType" enum.` on every
// real endpoint. UpdateEndpointModels must derive the enum from the REST bool.
func TestUpdateEndpointModels_DerivesFlashBootTypeFromRESTBool(t *testing.T) {
	stub := &updateModelsStub{
		// REST read: the real wire shape — "flashboot" bool, no "flashBootType".
		restJSON: `{
			"id": "ep-abc",
			"name": "my-ep",
			"templateId": "tpl-1",
			"gpuTypeIds": ["NVIDIA L4"],
			"workersMin": 0,
			"workersMax": 1,
			"flashboot": true
		}`,
		gpuIDs: "ADA_24",
	}
	client := stub.client(t)

	if _, err := client.UpdateEndpointModels("ep-abc", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stub.saveInput["flashBootType"]; got != "FLASHBOOT" {
		t.Errorf("expected flashBootType FLASHBOOT when REST flashboot=true, got %v", got)
	}
}

// A REST read never returns gpuIds, so the empty one that left made
// saveEndpoint reject every GPU endpoint update with "gpuId(s) is required for
// a gpu endpoint". gpuIds has to be read over GraphQL, and passed through
// untouched: it is not just a pool id but a whole gpu selection, and the "-"
// entries restricting an endpoint to a subset of a pool are unrepresentable in
// the gpuTypeIds REST reports. Reconstructing it from those (found live on
// PR #340) re-permitted the excluded gpus, so a model-reference change silently
// widened the endpoint's hardware.
func TestUpdateEndpointModels_PreservesGpuPoolExclusions(t *testing.T) {
	stub := &updateModelsStub{
		// an endpoint restricted to one gpu of the two-gpu AMPERE_48 pool: REST
		// reports only the allowed type, graphql the pool plus the exclusion.
		restJSON: `{
			"id": "ep-abc",
			"name": "my-ep",
			"templateId": "tpl-1",
			"gpuTypeIds": ["NVIDIA A40"],
			"workersMin": 0,
			"workersMax": 1
		}`,
		gpuIDs: "AMPERE_48,-NVIDIA RTX A6000",
	}
	client := stub.client(t)

	if _, err := client.UpdateEndpointModels("ep-abc", []string{"https://huggingface.co/org/model:main"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stub.saveInput["gpuIds"]; got != "AMPERE_48,-NVIDIA RTX A6000" {
		t.Errorf("gpuIds = %v, want the graphql value verbatim; an update must not widen the gpu selection", got)
	}
}

// A failed gpuIds read must stop the update. Writing what we could not read
// would drop the endpoint's gpu selection, and inferring one from the REST
// gpuTypeIds gets the whole mutation rejected with `Invalid GPU Pool ID`,
// masking the read failure that caused it (found live on PR #340).
func TestUpdateEndpointModels_GpuIDsReadFailureSkipsWrite(t *testing.T) {
	stub := &updateModelsStub{
		restJSON: `{
			"id": "ep-abc",
			"name": "my-ep",
			"gpuTypeIds": ["NVIDIA A40"],
			"workersMin": 0,
			"workersMax": 1
		}`,
		gpuIDsErr: "Something went wrong. Please try again later or contact support.",
	}
	client := stub.client(t)

	_, err := client.UpdateEndpointModels("ep-abc", nil)
	if err == nil {
		t.Fatal("expected an error when the gpuIds read fails")
	}
	if !strings.Contains(err.Error(), "failed to read endpoint gpu ids") {
		t.Errorf("unexpected error: %v", err)
	}
	if stub.saveCalled {
		t.Error("saveEndpoint must not be called when the gpuIds read fails")
	}
}

// saveEndpoint rejects an empty gpuIds on anything without instanceIds, so
// report that rather than sending a write that drops the gpu selection.
func TestUpdateEndpointModels_EmptyGpuIDsOnGpuEndpointSkipsWrite(t *testing.T) {
	stub := &updateModelsStub{
		restJSON: `{
			"id": "ep-abc",
			"name": "my-ep",
			"gpuTypeIds": ["NVIDIA A40"],
			"workersMin": 0,
			"workersMax": 1
		}`,
		gpuIDs: "",
	}
	client := stub.client(t)

	_, err := client.UpdateEndpointModels("ep-abc", nil)
	if err == nil {
		t.Fatal("expected an error when a gpu endpoint reports no gpuIds")
	}
	if !strings.Contains(err.Error(), "no gpuIds and no instanceIds") {
		t.Errorf("unexpected error: %v", err)
	}
	if stub.saveCalled {
		t.Error("saveEndpoint must not be called with an empty gpu selection")
	}
}

// A cpu endpoint has instanceIds and legitimately no gpuIds, so the empty-gpuIds
// guard above must not block it.
func TestUpdateEndpointModels_CPUEndpointNeedsNoGpuIDs(t *testing.T) {
	stub := &updateModelsStub{
		restJSON: `{
			"id": "ep-abc",
			"name": "my-ep",
			"templateId": "tpl-1",
			"instanceIds": ["cpu3c-2-4"],
			"workersMin": 0,
			"workersMax": 1
		}`,
		gpuIDs: "",
	}
	client := stub.client(t)

	if _, err := client.UpdateEndpointModels("ep-abc", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := stub.saveInput["gpuIds"]; got != "" {
		t.Errorf("gpuIds = %v, want empty for a cpu endpoint", got)
	}
	ids, _ := stub.saveInput["instanceIds"].([]interface{})
	if len(ids) != 1 || ids[0] != "cpu3c-2-4" {
		t.Errorf("instanceIds = %v, want the cpu instance preserved", stub.saveInput["instanceIds"])
	}
}

func TestDeleteEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	err := client.DeleteEndpoint("ep-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInvokeURLs(t *testing.T) {
	urls := invokeURLs("ep-abc")
	if urls == nil {
		t.Fatal("expected non-nil urls for a valid id")
	}
	if urls.Run != "https://api.runpod.ai/v2/ep-abc/run" {
		t.Errorf("run = %q", urls.Run)
	}
	if urls.RunSync != "https://api.runpod.ai/v2/ep-abc/runsync" {
		t.Errorf("runsync = %q", urls.RunSync)
	}
	if urls.Health != "https://api.runpod.ai/v2/ep-abc/health" {
		t.Errorf("health = %q", urls.Health)
	}
	if invokeURLs("") != nil {
		t.Error("expected nil urls for empty id")
	}
}

func TestInvokeURLs_HonorsInvokeURLOverride(t *testing.T) {
	// invoke is a separate service from the control plane, so it has its own
	// override. sloppy values must not produce malformed urls.
	tests := []struct {
		name    string
		env     string
		wantRun string
	}{
		{"plain", "https://staging.example.com/v2", "https://staging.example.com/v2/ep-abc/run"},
		{"trailing slash", "https://staging.example.com/v2/", "https://staging.example.com/v2/ep-abc/run"},
		{"repeated trailing slashes", "https://staging.example.com/v2//", "https://staging.example.com/v2/ep-abc/run"},
		{"surrounding whitespace", "  https://staging.example.com/v2  ", "https://staging.example.com/v2/ep-abc/run"},
		{"slash only falls back to prod", "/", ServerlessInvokeBaseURL + "/ep-abc/run"},
		{"empty falls back to prod", "", ServerlessInvokeBaseURL + "/ep-abc/run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(configenv.InvokeURLEnv, tt.env)

			urls := invokeURLs("ep-abc")
			if urls == nil {
				t.Fatal("expected non-nil urls for a valid id")
			}
			if urls.Run != tt.wantRun {
				t.Errorf("run = %q, want %q", urls.Run, tt.wantRun)
			}
			if want := strings.TrimSuffix(tt.wantRun, "/run") + "/runsync"; urls.RunSync != want {
				t.Errorf("runsync = %q, want %q", urls.RunSync, want)
			}
			if want := strings.TrimSuffix(tt.wantRun, "/run") + "/health"; urls.Health != want {
				t.Errorf("health = %q, want %q", urls.Health, want)
			}
		})
	}
}

func TestInvokeURLs_ConfigFileKey(t *testing.T) {
	// the config-file path must work, not just the env var. save/restore the one
	// key instead of viper.Reset() so this stays order-independent alongside the
	// other tests in this package that set global viper keys.
	prev := viper.Get("invokeUrl")
	t.Cleanup(func() { viper.Set("invokeUrl", prev) })
	t.Setenv(configenv.InvokeURLEnv, "")
	viper.Set("invokeUrl", "https://from-config.example.com/v2")

	urls := invokeURLs("ep-abc")
	if urls == nil {
		t.Fatal("expected non-nil urls for a valid id")
	}
	if urls.Run != "https://from-config.example.com/v2/ep-abc/run" {
		t.Errorf("run = %q", urls.Run)
	}
}

func TestGetEndpoint_PopulatesURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"ep-9","name":"x"}`))
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, _ := NewClient()
	client.baseURL = server.URL

	ep, err := client.GetEndpoint("ep-9", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.URLs == nil || ep.URLs.Run != "https://api.runpod.ai/v2/ep-9/run" {
		t.Errorf("expected populated invoke urls, got %+v", ep.URLs)
	}
}

func TestListEndpoints_PopulatesURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":"ep-1","name":"a"},{"id":"ep-2","name":"b"}]`))
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, _ := NewClient()
	client.baseURL = server.URL

	eps, err := client.ListEndpoints(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, ep := range eps {
		if ep.URLs == nil || ep.URLs.RunSync != "https://api.runpod.ai/v2/"+ep.ID+"/runsync" {
			t.Errorf("endpoint %s missing invoke urls: %+v", ep.ID, ep.URLs)
		}
	}
}

// The reported invoke urls are printed for people to curl, so an id with a
// path-significant character in it must not build a url addressing something else.
func TestInvokeURLsEscapeTheID(t *testing.T) {
	t.Setenv(configenv.InvokeURLEnv, "https://api.runpod.ai/v2")

	urls := invokeURLs("ep-1/../v1")
	if urls == nil {
		t.Fatal("expected urls for a non-empty id")
	}
	if want := "https://api.runpod.ai/v2/ep-1%2F..%2Fv1/run"; urls.Run != want {
		t.Errorf("run url = %q, want %q", urls.Run, want)
	}

	// and an ordinary id is untouched, or every existing caller's url changes.
	if urls := invokeURLs("ep-abc123"); urls.Health != "https://api.runpod.ai/v2/ep-abc123/health" {
		t.Errorf("health url = %q, want it unescaped for an ordinary id", urls.Health)
	}
}
