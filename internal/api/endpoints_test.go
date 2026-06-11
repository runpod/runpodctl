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
		json.NewEncoder(w).Encode([]Endpoint{
			{ID: "ep-1", Name: "endpoint-1"},
			{ID: "ep-2", Name: "endpoint-2"},
		})
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
}

func TestGetEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/endpoints/ep-123" {
			t.Errorf("expected /endpoints/ep-123, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(Endpoint{
			ID:         "ep-123",
			Name:       "my-endpoint",
			WorkersMin: 0,
			WorkersMax: 3,
		})
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
}

func TestCreateEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var req EndpointCreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(Endpoint{
			ID:         "new-ep-id",
			Name:       req.Name,
			TemplateID: req.TemplateID,
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	endpoint, err := client.CreateEndpoint(&EndpointCreateRequest{
		Name:       "test-endpoint",
		TemplateID: "tpl-123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint.ID != "new-ep-id" {
		t.Errorf("expected new-ep-id, got %s", endpoint.ID)
	}
}

func TestEndpointCreateGQLInputOmitsEmptyName(t *testing.T) {
	data, err := json.Marshal(EndpointCreateGQLInput{
		TemplateID: "tpl-123",
	})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	if strings.Contains(string(data), `"name"`) {
		t.Fatalf("expected empty name to be omitted, got %s", data)
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
