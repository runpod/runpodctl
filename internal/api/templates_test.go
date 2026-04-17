package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func intPtr(v int) *int {
	return &v
}

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
			ID:                "new-tpl-id",
			Name:              req.Name,
			ImageName:         req.ImageName,
			VolumeMountPath:   req.VolumeMountPath,
			ContainerDiskInGb: req.ContainerDiskInGb,
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	template, err := client.CreateTemplate(&TemplateCreateRequest{
		Name:              "test-template",
		ImageName:         "runpod/pytorch",
		VolumeMountPath:   "/models",
		ContainerDiskInGb: 30,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if template.ID != "new-tpl-id" {
		t.Errorf("expected new-tpl-id, got %s", template.ID)
	}
	if template.VolumeMountPath != "/models" {
		t.Errorf("expected /models, got %s", template.VolumeMountPath)
	}
	if template.ContainerDiskInGb != 30 {
		t.Errorf("expected 30, got %d", template.ContainerDiskInGb)
	}
}

func TestUpdateTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/templates/tpl-123" {
			t.Errorf("expected /templates/tpl-123, got %s", r.URL.Path)
		}

		var req TemplateUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ContainerDiskInGb == nil || *req.ContainerDiskInGb != 40 {
			t.Fatalf("expected containerDiskInGb 40, got %#v", req.ContainerDiskInGb)
		}

		json.NewEncoder(w).Encode(Template{
			ID:                "tpl-123",
			ContainerDiskInGb: *req.ContainerDiskInGb,
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	template, err := client.UpdateTemplate("tpl-123", &TemplateUpdateRequest{
		ContainerDiskInGb: intPtr(40),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if template.ContainerDiskInGb != 40 {
		t.Errorf("expected 40, got %d", template.ContainerDiskInGb)
	}
}

func TestUpdateTemplate_AllowsZeroContainerDisk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req TemplateUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ContainerDiskInGb == nil {
			t.Fatal("expected containerDiskInGb to be present")
		}
		if *req.ContainerDiskInGb != 0 {
			t.Fatalf("expected containerDiskInGb 0, got %d", *req.ContainerDiskInGb)
		}

		json.NewEncoder(w).Encode(Template{
			ID:                "tpl-123",
			ContainerDiskInGb: *req.ContainerDiskInGb,
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	template, err := client.UpdateTemplate("tpl-123", &TemplateUpdateRequest{
		ContainerDiskInGb: intPtr(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if template.ContainerDiskInGb != 0 {
		t.Errorf("expected 0, got %d", template.ContainerDiskInGb)
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

func TestTemplateFromGraphQL(t *testing.T) {
	source := &templateGraphQL{
		ID:                "tpl-graph",
		Name:              "graph-template",
		ImageName:         "runpod/graph",
		Readme:            "hello",
		Ports:             templatePorts{"22/tcp"},
		Env:               []templateEnvPair{{Key: "A", Value: "1"}, {Key: "", Value: "ignore"}},
		ContainerDiskInGb: 10,
		VolumeInGb:        20,
		VolumeMountPath:   "/data",
	}

	template := templateFromGraphQL(source)
	if template == nil {
		t.Fatal("expected template, got nil")
	}
	if template.ID != "tpl-graph" {
		t.Errorf("expected tpl-graph, got %s", template.ID)
	}
	if template.Readme != "hello" {
		t.Errorf("expected readme to be set")
	}
	if template.Env["A"] != "1" {
		t.Errorf("expected env A to be set")
	}
	if _, ok := template.Env[""]; ok {
		t.Errorf("expected empty env key to be skipped")
	}
}

func TestTemplatePortsUnmarshal(t *testing.T) {
	var ports templatePorts
	if err := json.Unmarshal([]byte(`"22/tcp, 80/http"`), &ports); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 2 || ports[0] != "22/tcp" || ports[1] != "80/http" {
		t.Errorf("unexpected ports: %v", ports)
	}

	ports = nil
	if err := json.Unmarshal([]byte(`["22/tcp","80/http"]`), &ports); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 2 || ports[0] != "22/tcp" || ports[1] != "80/http" {
		t.Errorf("unexpected ports: %v", ports)
	}
}
