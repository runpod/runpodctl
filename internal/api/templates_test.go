package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListTemplates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/templates" {
			t.Errorf("expected /templates, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]Template{
			{ID: "tpl-1", Name: "template-1"},
			{ID: "tpl-2", Name: "template-2"},
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	templates, err := client.ListTemplates()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(templates) != 2 {
		t.Errorf("expected 2 templates, got %d", len(templates))
	}
}

func TestGetTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Template{
			ID:        "tpl-123",
			Name:      "my-template",
			ImageName: "runpod/pytorch",
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	template, err := client.GetTemplate("tpl-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if template.ID != "tpl-123" {
		t.Errorf("expected tpl-123, got %s", template.ID)
	}
}

func TestCreateTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var req TemplateCreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(Template{
			ID:        "new-tpl-id",
			Name:      req.Name,
			ImageName: req.ImageName,
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	template, err := client.CreateTemplate(&TemplateCreateRequest{
		Name:      "test-template",
		ImageName: "runpod/pytorch",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if template.ID != "new-tpl-id" {
		t.Errorf("expected new-tpl-id, got %s", template.ID)
	}
}

func TestDeleteTemplate(t *testing.T) {
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

	err := client.DeleteTemplate("tpl-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
