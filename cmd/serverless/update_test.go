package serverless

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestUpdateCmd_HasTemplateIDFlag(t *testing.T) {
	flag := updateCmd.Flags().Lookup("template-id")
	if flag == nil {
		t.Fatal("expected template-id flag")
	}
}

func TestUpdateCmd_HasModelReferenceFlag(t *testing.T) {
	if flag := updateCmd.Flags().Lookup("model-reference"); flag == nil {
		t.Fatal("expected model-reference flag")
	}
}

func TestUpdateCmd_HasClearModelsFlag(t *testing.T) {
	if flag := updateCmd.Flags().Lookup("clear-models"); flag == nil {
		t.Fatal("expected clear-models flag")
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w

	fn()

	_ = w.Close()
	os.Stderr = origStderr
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	_ = r.Close()

	return string(data)
}

func resetUpdateVars(t *testing.T) {
	t.Helper()
	origName := updateName
	origTemplateID := updateTemplateID
	origWorkersMin := updateWorkersMin
	origWorkersMax := updateWorkersMax
	origIdleTimeout := updateIdleTimeout
	origScaleBy := updateScaleBy
	origScaleThreshold := updateScaleThreshold
	origModelRefs := updateModelRefs
	origClearModels := updateClearModels
	t.Cleanup(func() {
		updateName = origName
		updateTemplateID = origTemplateID
		updateWorkersMin = origWorkersMin
		updateWorkersMax = origWorkersMax
		updateIdleTimeout = origIdleTimeout
		updateScaleBy = origScaleBy
		updateScaleThreshold = origScaleThreshold
		updateModelRefs = origModelRefs
		updateClearModels = origClearModels
	})
}

func TestRunUpdate_WarnsWhenTemplateSwapFailsAfterRESTUpdate(t *testing.T) {
	resetUpdateVars(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/endpoints/ep-123":
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode rest request: %v", err)
			}
			if body["name"] != "patched-name" {
				t.Fatalf("expected name patched-name, got %#v", body["name"])
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   "ep-123",
				"name": "patched-name",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []map[string]interface{}{
					{"message": "template swap failed"},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	viper.Set("restApiUrl", server.URL)
	viper.Set("apiUrl", server.URL)
	t.Cleanup(func() {
		viper.Set("restApiUrl", "")
		viper.Set("apiUrl", "")
	})

	updateName = "patched-name"
	updateTemplateID = "tpl-456"
	updateWorkersMin = -1
	updateWorkersMax = -1
	updateIdleTimeout = -1
	updateScaleBy = ""
	updateScaleThreshold = -1
	updateModelRefs = nil
	updateClearModels = false

	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")

	var runErr error
	stderr := captureStderr(t, func() {
		runErr = runUpdate(cmd, []string{"ep-123"})
	})

	if runErr == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(runErr.Error(), "failed to update endpoint template: graphql error: template swap failed") {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if !strings.Contains(stderr, "warning: endpoint rest fields were updated, but template swap failed") {
		t.Fatalf("expected warning, got %q", stderr)
	}
	if strings.Contains(stderr, `{"error":`) {
		t.Fatalf("expected no json error output, got %q", stderr)
	}
}

func TestRunUpdate_ClearModelsAndModelReferenceMutuallyExclusive(t *testing.T) {
	resetUpdateVars(t)

	updateModelRefs = []string{"https://huggingface.co/org/model:main"}
	updateClearModels = true
	updateWorkersMin = -1
	updateWorkersMax = -1
	updateIdleTimeout = -1
	updateScaleThreshold = -1

	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")

	err := runUpdate(cmd, []string{"ep-123"})
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunUpdate_ModelReferences(t *testing.T) {
	resetUpdateVars(t)

	var gqlBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/endpoints/ep-123":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   "ep-123",
				"name": "my-endpoint",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/":
			if err := json.NewDecoder(r.Body).Decode(&gqlBody); err != nil {
				t.Fatalf("decode gql request: %v", err)
			}
			// could be the UpdateEndpointModels call or the final GET (which uses REST)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"saveEndpoint": map[string]interface{}{
						"id":              "ep-123",
						"name":            "my-endpoint",
						"modelReferences": []string{"https://huggingface.co/org/model:main"},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	viper.Set("restApiUrl", server.URL)
	viper.Set("apiUrl", server.URL)
	t.Cleanup(func() {
		viper.Set("restApiUrl", "")
		viper.Set("apiUrl", "")
	})

	updateModelRefs = []string{"https://huggingface.co/org/model:main"}
	updateClearModels = false
	updateWorkersMin = -1
	updateWorkersMax = -1
	updateIdleTimeout = -1
	updateScaleThreshold = -1

	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")

	err := runUpdate(cmd, []string{"ep-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify the saveEndpoint mutation received the expected model references
	vars, _ := gqlBody["variables"].(map[string]interface{})
	input, _ := vars["input"].(map[string]interface{})
	refs, _ := input["modelReferences"].([]interface{})
	if len(refs) != 1 || refs[0] != "https://huggingface.co/org/model:main" {
		t.Fatalf("expected modelReferences to contain the provided ref, got %#v", refs)
	}
}
