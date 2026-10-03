package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateTemplateIncludesRegistryAuthID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got := payload["registry"]; got != "registry-123" {
			t.Fatalf("registry = %#v, want registry-123", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "tpl-123"})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.v2BaseURL = server.URL

	_, err = client.CreateTemplate(&TemplateCreateRequest{
		Name:                    "private-template",
		ImageName:               "registry.example.com/team/image:tag",
		ContainerRegistryAuthID: "registry-123",
	})
	if err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
}

func TestUpdateTemplateCanClearRegistryAuthID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("expected PATCH, got %s", r.Method)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		// v2 clears the registry with an empty string; it ignores null
		value, exists := payload["registry"]
		if !exists {
			t.Fatal("expected registry to be present")
		}
		if value != "" {
			t.Fatalf("registry = %#v, want empty string", value)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "tpl-123"})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.v2BaseURL = server.URL

	empty := ""
	_, err = client.UpdateTemplate("tpl-123", &TemplateUpdateRequest{
		ContainerRegistryAuthID: &empty,
	})
	if err != nil {
		t.Fatalf("UpdateTemplate() error = %v", err)
	}
}
