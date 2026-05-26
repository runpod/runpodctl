package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestUpdateEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/endpoints/ep-123" {
			t.Errorf("expected /endpoints/ep-123, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(Endpoint{
			ID:         "ep-123",
			WorkersMax: 5,
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	endpoint, err := client.UpdateEndpoint("ep-123", &EndpointUpdateRequest{
		WorkersMax: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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

func TestUpdateEndpointModels_RoundTripsConfig(t *testing.T) {
	oldAPIURL := viper.GetString("apiUrl")
	t.Cleanup(func() { viper.Set("apiUrl", oldAPIURL) })

	var gqlInput map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/endpoints/"):
			// REST read: return wire shape with custom config and a bare-string networkVolumeId.
			w.Write([]byte(`{
				"id": "ep-abc",
				"name": "my-ep",
				"templateId": "tpl-1",
				"gpuIds": "ADA_24",
				"workersMin": 1,
				"workersMax": 5,
				"idleTimeout": 42,
				"scalerType": "REQUEST_COUNT",
				"scalerValue": 9,
				"networkVolumeId": "vol-9",
				"networkVolumeIds": ["vol-9"]
			}`))
		case r.Method == http.MethodPost:
			// GraphQL saveEndpoint call.
			var body struct {
				Variables struct {
					Input map[string]interface{} `json:"input"`
				} `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode gql body: %v", err)
			}
			gqlInput = body.Variables.Input
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"saveEndpoint": map[string]interface{}{
						"id":   "ep-abc",
						"name": "my-ep",
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	viper.Set("apiUrl", server.URL)

	client, _ := NewClient()
	client.baseURL = server.URL

	_, err := client.UpdateEndpointModels("ep-abc", []string{"https://huggingface.co/org/model:main"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All existing config fields must be present in the mutation input.
	checks := map[string]interface{}{
		"id":           "ep-abc",
		"name":         "my-ep",
		"templateId":   "tpl-1",
		"gpuIds":       "ADA_24",
		"workersMax":   float64(5),
		"idleTimeout":  float64(42),
		"scalerType":   "REQUEST_COUNT",
		"scalerValue":  float64(9),
	}
	for field, want := range checks {
		if got := gqlInput[field]; got != want {
			t.Errorf("input.%s = %v, want %v", field, got, want)
		}
	}

	// modelReferences must carry the new value, not the old one.
	refs, _ := gqlInput["modelReferences"].([]interface{})
	if len(refs) != 1 || refs[0] != "https://huggingface.co/org/model:main" {
		t.Errorf("unexpected modelReferences: %v", gqlInput["modelReferences"])
	}

	// networkVolumeIds must be passed as objects, not bare strings.
	nvids, _ := gqlInput["networkVolumeIds"].([]interface{})
	if len(nvids) != 1 {
		t.Fatalf("expected 1 networkVolumeId, got %v", gqlInput["networkVolumeIds"])
	}
	nvobj, _ := nvids[0].(map[string]interface{})
	if nvobj["networkVolumeId"] != "vol-9" {
		t.Errorf("expected networkVolumeId vol-9, got %v", nvobj)
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
