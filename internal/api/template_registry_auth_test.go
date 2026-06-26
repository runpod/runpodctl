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
		if got := payload["containerRegistryAuthId"]; got != "registry-123" {
			t.Fatalf("containerRegistryAuthId = %#v, want registry-123", got)
		}
		_ = json.NewEncoder(w).Encode(Template{ID: "tpl-123"})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.baseURL = server.URL

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
		value, exists := payload["containerRegistryAuthId"]
		if !exists {
			t.Fatal("expected containerRegistryAuthId to be present")
		}
		if value != "" {
			t.Fatalf("containerRegistryAuthId = %#v, want empty string", value)
		}
		_ = json.NewEncoder(w).Encode(Template{ID: "tpl-123"})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.baseURL = server.URL

	empty := ""
	_, err = client.UpdateTemplate("tpl-123", &TemplateUpdateRequest{
		ContainerRegistryAuthID: &empty,
	})
	if err != nil {
		t.Fatalf("UpdateTemplate() error = %v", err)
	}
}
