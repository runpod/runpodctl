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

func TestRunUpdate_WarnsWhenTemplateSwapFailsAfterRESTUpdate(t *testing.T) {
	origName := updateName
	origTemplateID := updateTemplateID
	origWorkersMin := updateWorkersMin
	origWorkersMax := updateWorkersMax
	origIdleTimeout := updateIdleTimeout
	origScalerType := updateScalerType
	origScalerValue := updateScalerValue
	t.Cleanup(func() {
		updateName = origName
		updateTemplateID = origTemplateID
		updateWorkersMin = origWorkersMin
		updateWorkersMax = origWorkersMax
		updateIdleTimeout = origIdleTimeout
		updateScalerType = origScalerType
		updateScalerValue = origScalerValue
	})

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
	updateScalerType = ""
	updateScalerValue = -1

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
