package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestUpdateTemplatePortLabelsPreservesStateAndAppliesOverrides(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var request GraphQLInput
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		switch {
		case strings.Contains(request.Query, "GetTemplateForPortLabels"):
			_, _ = w.Write([]byte(`{
				"data": {"myself": {"podTemplates": [{
					"id": "tpl-123",
					"name": "old-name",
					"imageName": "old-image",
					"dockerArgs": "--serve",
					"env": [{"key": "OLD", "value": "1"}],
					"ports": "22/tcp,8888/http",
					"volumeMountPath": "/workspace",
					"volumeInGb": 20,
					"containerDiskInGb": 30,
					"containerRegistryAuthId": "old-auth",
					"startJupyter": true,
					"startSsh": true,
					"startScript": "echo ready",
					"isServerless": false,
					"isPublic": false,
					"readme": "old readme",
					"advancedStart": true,
					"category": "NVIDIA"
				}]}}
			}`))
		case strings.Contains(request.Query, "UpdateTemplatePortLabels"):
			input, ok := request.Variables["input"].(map[string]interface{})
			if !ok {
				t.Fatalf("input = %#v", request.Variables["input"])
			}
			assertStringValue(t, input, "name", "new-name")
			assertStringValue(t, input, "imageName", "new-image")
			assertStringValue(t, input, "containerRegistryAuthId", "new-auth")
			assertStringValue(t, input, "ports", "22/tcp,9000/http")
			assertStringValue(t, input, "dockerArgs", "--serve")
			assertStringValue(t, input, "volumeMountPath", "/workspace")
			assertStringValue(t, input, "startScript", "echo ready")
			assertStringValue(t, input, "category", "NVIDIA")
			if input["containerDiskInGb"] != float64(40) {
				t.Fatalf("containerDiskInGb = %#v, want 40", input["containerDiskInGb"])
			}

			env, ok := input["env"].([]interface{})
			if !ok || len(env) != 2 {
				t.Fatalf("env = %#v, want two entries", input["env"])
			}
			firstEnv := env[0].(map[string]interface{})
			secondEnv := env[1].(map[string]interface{})
			if firstEnv["key"] != "A" || secondEnv["key"] != "Z" {
				t.Fatalf("env order = %#v, want sorted keys", env)
			}

			labels, ok := input["portsConfig"].([]interface{})
			if !ok || len(labels) != 2 {
				t.Fatalf("portsConfig = %#v, want two labels", input["portsConfig"])
			}
			firstLabel := labels[0].(map[string]interface{})
			secondLabel := labels[1].(map[string]interface{})
			if firstLabel["port"] != "22" || firstLabel["name"] != "SSH" {
				t.Fatalf("first label = %#v", firstLabel)
			}
			if secondLabel["port"] != "9000" || secondLabel["name"] != "API" {
				t.Fatalf("second label = %#v", secondLabel)
			}

			_, _ = w.Write([]byte(`{"data":{"saveTemplate":{"id":"tpl-123"}}}`))
		default:
			t.Fatalf("unexpected graphql query: %s", request.Query)
		}
	}))
	defer server.Close()

	client := &GraphQLClient{
		url:        server.URL,
		apiKey:     "test-key",
		httpClient: server.Client(),
	}

	newName := "new-name"
	newImage := "new-image"
	newPorts := []string{"22/tcp", "9000/http"}
	newEnv := map[string]string{"Z": "26", "A": "1"}
	newDiskSize := 40
	newAuth := "new-auth"
	err := client.UpdateTemplatePortLabels(
		"tpl-123",
		[]TemplatePortConfig{{Port: "22/tcp", Name: "SSH"}, {Port: "9000", Name: "API"}},
		&TemplatePortLabelOverrides{
			Name:                    &newName,
			ImageName:               &newImage,
			Ports:                   &newPorts,
			Env:                     &newEnv,
			ContainerDiskInGb:       &newDiskSize,
			ContainerRegistryAuthID: &newAuth,
		},
	)
	if err != nil {
		t.Fatalf("UpdateTemplatePortLabels() error = %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
}

// TestUpdateTemplatePortLabelsPreservesStartCommand pins the highest-risk path
// of the read-subset/write-whole label write: applying a label must NOT wipe the
// template's start command. The CLI accepts dockerStartCmd/dockerEntrypoint on
// the REST create/update path; the backend stores the resulting start command in
// GraphQL's `dockerArgs` (there is no separate dockerStartCmd/dockerEntrypoint on
// the GraphQL PodTemplate). Because saveTemplate rewrites the whole object, the
// only thing protecting the start command is that getTemplateSaveState reads
// `dockerArgs` and writes it straight back. Here we apply a label WITHOUT any
// override touching the command and assert the exact dockerArgs + startScript
// round-trip unchanged — a regression here means a label write silently clears a
// template's start command.
func TestUpdateTemplatePortLabelsPreservesStartCommand(t *testing.T) {
	const startCmd = `sh -c "python -u app.py"`
	const startScript = "echo booting"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request GraphQLInput
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch {
		case strings.Contains(request.Query, "GetTemplateForPortLabels"):
			_, _ = w.Write([]byte(`{"data":{"myself":{"podTemplates":[{
				"id":"tpl-cmd",
				"name":"cmd-template",
				"imageName":"img",
				"dockerArgs":` + strconv.Quote(startCmd) + `,
				"env":[],
				"ports":"22/tcp",
				"containerDiskInGb":20,
				"startScript":` + strconv.Quote(startScript) + `,
				"category":"NVIDIA"
			}]}}}`))
		case strings.Contains(request.Query, "UpdateTemplatePortLabels"):
			input, ok := request.Variables["input"].(map[string]interface{})
			if !ok {
				t.Fatalf("input = %#v", request.Variables["input"])
			}
			// The start command must survive untouched.
			assertStringValue(t, input, "dockerArgs", startCmd)
			assertStringValue(t, input, "startScript", startScript)
			_, _ = w.Write([]byte(`{"data":{"saveTemplate":{"id":"tpl-cmd"}}}`))
		default:
			t.Fatalf("unexpected graphql query: %s", request.Query)
		}
	}))
	defer server.Close()

	client := &GraphQLClient{url: server.URL, apiKey: "test-key", httpClient: server.Client()}
	// Apply a label with NO overrides — nothing here should touch the command.
	err := client.UpdateTemplatePortLabels(
		"tpl-cmd",
		[]TemplatePortConfig{{Port: "22/tcp", Name: "SSH"}},
		nil,
	)
	if err != nil {
		t.Fatalf("UpdateTemplatePortLabels() error = %v", err)
	}
}

func TestUpdateTemplatePortLabelsRejectsUnknownPort(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		_, _ = w.Write([]byte(`{"data":{"myself":{"podTemplates":[{
			"id":"tpl-123",
			"name":"test",
			"imageName":"image",
			"dockerArgs":"",
			"env":[],
			"ports":"22/tcp",
			"volumeInGb":0,
			"containerDiskInGb":20
		}]}}}`))
	}))
	defer server.Close()

	client := &GraphQLClient{url: server.URL, apiKey: "test-key", httpClient: server.Client()}
	err := client.UpdateTemplatePortLabels("tpl-123", []TemplatePortConfig{{Port: "8888", Name: "Jupyter"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "does not match any template port") {
		t.Fatalf("error = %v, want unknown port error", err)
	}
	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
}

func TestTemplateFromGraphQLIncludesRegistryAuthAndPortLabels(t *testing.T) {
	template := templateFromGraphQL(&templateGraphQL{
		ID:                      "tpl-123",
		ContainerRegistryAuthID: "registry-123",
		PortsConfig:             []TemplatePortConfig{{Port: "22", Name: "SSH"}},
	})
	if template.ContainerRegistryAuthID != "registry-123" {
		t.Fatalf("ContainerRegistryAuthID = %q", template.ContainerRegistryAuthID)
	}
	if len(template.PortsConfig) != 1 || template.PortsConfig[0].Name != "SSH" {
		t.Fatalf("PortsConfig = %#v", template.PortsConfig)
	}
}

func assertStringValue(t *testing.T, values map[string]interface{}, key, want string) {
	t.Helper()
	if got := values[key]; got != want {
		t.Fatalf("%s = %#v, want %q", key, got, want)
	}
}
