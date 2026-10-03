package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

func intPtr(v int) *int {
	return &v
}

func TestListTemplates(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/templates" {
			t.Errorf("expected /templates, got %s", r.URL.Path)
		}
		pages++
		if r.URL.Query().Get("cursor") == "" {
			_, _ = io.WriteString(w, `{"templates":[{"id":"tpl-1","name":"template-1","image":"img:1"}],
				"pagination":{"hasNextPage":true,"nextCursor":"c2"}}`)
			return
		}
		if got := r.URL.Query().Get("cursor"); got != "c2" {
			t.Errorf("cursor = %q, want c2", got)
		}
		_, _ = io.WriteString(w, `{"templates":[{"id":"tpl-2","name":"template-2","image":"img:2"}],
			"pagination":{"hasNextPage":false,"nextCursor":null}}`)
	}))
	defer server.Close()

	templates, err := newV2TestClient(t, server).ListTemplates()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pages != 2 || len(templates) != 2 || templates[1].ImageName != "img:2" {
		t.Fatalf("pages = %d, templates = %+v; want both pages read", pages, templates)
	}
}

func TestGetTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/templates/tpl-123" {
			t.Errorf("expected /templates/tpl-123, got %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"tpl-123","name":"my-template","image":"runpod/pytorch",
			"ports":["8888/http","22/tcp"],"env":{"A":"1"},"disk":20,"registry":"reg-1",
			"mounts":{"persistent":{"size":30,"path":"/workspace"}},"entrypoint":["bash"],"cmd":["-c","run"],
			"serverless":false,"public":true,"category":"NVIDIA"}`)
	}))
	defer server.Close()
	graphql := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"podTemplate":{"id":"tpl-123","readme":"# hi","isRunpod":true,
			"portsConfig":[{"port":"22","name":"SSH"}]}}}`)
	}))
	defer graphql.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", graphql.URL)

	template, err := newV2TestClient(t, server).GetTemplate("tpl-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Template{
		ID: "tpl-123", Name: "my-template", ImageName: "runpod/pytorch", IsPublic: true, IsRunpod: true,
		Category: "NVIDIA", Ports: []string{"8888/http", "22/tcp"}, Env: map[string]string{"A": "1"},
		PortsConfig:      []TemplatePortConfig{{Port: "22", Name: "SSH"}},
		DockerEntrypoint: []string{"bash"}, DockerStartCmd: []string{"-c", "run"},
		ContainerDiskInGb: 20, ContainerRegistryAuthID: "reg-1", VolumeInGb: 30, VolumeMountPath: "/workspace",
		Readme: "# hi",
	}
	if !reflect.DeepEqual(*template, want) {
		t.Fatalf("template =\n%+v\nwant\n%+v", *template, want)
	}
}

func TestCreateTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/templates" {
			t.Errorf("expected POST /templates, got %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		want := map[string]interface{}{
			// no ports given: v1's default is sent, since v2 has none
			"name": "test-template", "image": "runpod/pytorch", "serverless": false, "disk": float64(30),
			"ports":  []interface{}{"8888/http", "22/tcp"},
			"mounts": map[string]interface{}{"persistent": map[string]interface{}{"size": float64(20), "path": "/models"}},
		}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("body = %v, want %v", body, want)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"new-tpl-id","name":"test-template","image":"runpod/pytorch","disk":30,
			"mounts":{"persistent":{"size":20,"path":"/models"}}}`)
	}))
	defer server.Close()

	template, err := newV2TestClient(t, server).CreateTemplate(&TemplateCreateRequest{
		Name:              "test-template",
		ImageName:         "runpod/pytorch",
		VolumeInGb:        20,
		VolumeMountPath:   "/models",
		ContainerDiskInGb: 30,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if template.ID != "new-tpl-id" || template.VolumeMountPath != "/models" || template.ContainerDiskInGb != 30 {
		t.Errorf("template = %+v", *template)
	}
}

func TestUpdateTemplate(t *testing.T) {
	tests := []struct {
		name string
		disk int
	}{
		{name: "sets container disk", disk: 40},
		// v1 accepted 0; v2's minimum is 1, but the value is still sent so the api,
		// not the cli, decides
		{name: "sends a zero container disk", disk: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch || r.URL.Path != "/templates/tpl-123" {
					t.Errorf("expected PATCH /templates/tpl-123, got %s %s", r.Method, r.URL.Path)
				}
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				if disk, ok := body["disk"]; !ok || disk != float64(tt.disk) {
					t.Fatalf("disk = %#v (present %v), want %d", disk, ok, tt.disk)
				}
				_, _ = io.WriteString(w, `{"id":"tpl-123","disk":`+strconv.Itoa(tt.disk)+`}`)
			}))
			defer server.Close()

			template, err := newV2TestClient(t, server).UpdateTemplate("tpl-123", &TemplateUpdateRequest{
				ContainerDiskInGb: intPtr(tt.disk),
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if template.ContainerDiskInGb != tt.disk {
				t.Errorf("expected %d, got %d", tt.disk, template.ContainerDiskInGb)
			}
		})
	}
}

func TestDeleteTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/templates/tpl-123" {
			t.Errorf("expected DELETE /templates/tpl-123, got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := newV2TestClient(t, server).DeleteTemplate("tpl-123"); err != nil {
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
