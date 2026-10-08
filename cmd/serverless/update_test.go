package serverless

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
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

// updateServer is a fake control plane for runUpdate: rest v2 serves the
// endpoint (GET/PATCH /serverless/ep-123, plus the gpu catalog), graphql (POST
// /) serves the template swap and the model-reference round trip.
type updateServer struct {
	endpoint  string // raw v2 endpoint json
	patchBody map[string]interface{}
	renamed   map[string]interface{} // v1 rename body
	gql       func(query string, body map[string]interface{}) string
}

func (s *updateServer) start(t *testing.T) {
	t.Helper()
	if s.endpoint == "" {
		s.endpoint = `{"id":"ep-123","name":"my-endpoint","flashboot":"OFF","timeout":600000,
			"scaling":{"type":"REQUEST_COUNT","requestCount":9},
			"workers":{"min":0,"max":5,"idleTimeout":42}}`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/catalog/gpus":
			_, _ = w.Write([]byte(`{"gpus":[]}`))
		case r.URL.Path == "/serverless/ep-123" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(s.endpoint))
		case r.URL.Path == "/serverless/ep-123" && r.Method == http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&s.patchBody); err != nil {
				t.Errorf("decode patch body: %v", err)
			}
			_, _ = w.Write([]byte(s.endpoint))
		case r.URL.Path == "/endpoints/ep-123" && r.Method == http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&s.renamed); err != nil {
				t.Errorf("decode rename body: %v", err)
			}
			_, _ = w.Write([]byte(`{}`))
		case r.URL.Path == "/" && r.Method == http.MethodPost && s.gql != nil:
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode gql body: %v", err)
				return
			}
			query, _ := body["query"].(string)
			_, _ = w.Write([]byte(s.gql(query, body)))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_REST_V2_URL", server.URL)
	viper.Set("restApiUrl", server.URL)
	viper.Set("apiUrl", server.URL)
	t.Cleanup(func() {
		viper.Set("restApiUrl", "")
		viper.Set("apiUrl", "")
	})
}

// setUpdateFlags sets every update flag to unset, then applies fn.
func setUpdateFlags(fn func()) {
	updateName = ""
	updateTemplateID = ""
	updateWorkersMin = -1
	updateWorkersMax = -1
	updateIdleTimeout = -1
	updateScaleBy = ""
	updateScaleThreshold = -1
	updateModelRefs = nil
	updateClearModels = false
	if fn != nil {
		fn()
	}
}

func jsonOutputCmd(format string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", format, "")
	return cmd
}

func TestRunUpdate_WarnsWhenTemplateSwapFailsAfterRESTUpdate(t *testing.T) {
	resetUpdateVars(t)
	s := &updateServer{gql: func(string, map[string]interface{}) string {
		return `{"errors":[{"message":"template swap failed"}]}`
	}}
	s.start(t)
	setUpdateFlags(func() {
		updateName = "patched-name"
		updateTemplateID = "tpl-456"
	})

	var runErr error
	stderr := captureStderr(t, func() {
		runErr = runUpdate(jsonOutputCmd("json"), []string{"ep-123"})
	})

	if s.renamed["name"] != "patched-name" {
		t.Fatalf("expected name patched-name, got %#v", s.renamed)
	}
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
	setUpdateFlags(func() {
		updateModelRefs = []string{"https://huggingface.co/org/model:main"}
		updateClearModels = true
	})

	err := runUpdate(jsonOutputCmd("json"), []string{"ep-123"})
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// distinct values: identical ones would hide a flag wired to the wrong field.
func TestRunUpdate_AllNumericFlagsAreWired(t *testing.T) {
	resetUpdateVars(t)
	s := &updateServer{}
	s.start(t)
	setUpdateFlags(func() {
		updateName = "renamed"
		updateWorkersMin = 0
		updateWorkersMax = 7
		updateIdleTimeout = 30
		updateScaleBy = "delay"
		updateScaleThreshold = 4
	})

	if err := runUpdate(jsonOutputCmd("json"), []string{"ep-123"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.renamed["name"] != "renamed" {
		t.Errorf("name = %#v", s.renamed)
	}
	workers, _ := s.patchBody["workers"].(map[string]interface{})
	wantWorkers := map[string]float64{"min": 0, "max": 7, "idleTimeout": 30}
	for field, want := range wantWorkers {
		if got, ok := workers[field]; !ok || got != want {
			t.Errorf("workers.%s = %#v, want %v (body %#v)", field, got, want, s.patchBody)
		}
	}
	scaling, _ := s.patchBody["scaling"].(map[string]interface{})
	if scaling["type"] != "QUEUE_DELAY" || scaling["queueDelay"] != float64(4) {
		t.Errorf("scaling = %#v", scaling)
	}
}

func TestRunUpdate_WorkersMinZeroIsSent(t *testing.T) {
	resetUpdateVars(t)
	s := &updateServer{endpoint: `{"id":"ep-123","name":"x","scaling":{"type":"QUEUE_DELAY","queueDelay":4},"workers":{"min":2,"max":5,"idleTimeout":5}}`}
	s.start(t)
	setUpdateFlags(func() { updateWorkersMin = 0 })

	if err := runUpdate(jsonOutputCmd("json"), []string{"ep-123"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// see issue #298.
	workers, _ := s.patchBody["workers"].(map[string]interface{})
	if got, ok := workers["min"]; !ok || got != float64(0) {
		t.Fatalf("expected workers.min 0, got %#v", s.patchBody)
	}
	if workers["max"] != float64(5) {
		t.Errorf("workers.max must keep its current value, got %#v", workers["max"])
	}
}

func TestRunUpdate_RejectsOutOfRangeNumericFlags(t *testing.T) {
	cases := []struct {
		name           string
		idleTimeout    int
		scaleThreshold int
		wantErr        string
	}{
		{"idle timeout zero", 0, -1, "--idle-timeout must be between 1 and 3600 seconds"},
		{"idle timeout too large", 3601, -1, "--idle-timeout must be between 1 and 3600 seconds"},
		{"scale threshold zero", -1, 0, "--scale-threshold must be at least 1"},
	}

	// boundary values: an off-by-one in the guard would slip past otherwise.
	accepted := []struct {
		name           string
		idleTimeout    int
		scaleThreshold int
		block, field   string
		wantValue      float64
	}{
		{"idle timeout min", 1, -1, "workers", "idleTimeout", 1},
		{"idle timeout max", 3600, -1, "workers", "idleTimeout", 3600},
		{"scale threshold min", -1, 1, "scaling", "requestCount", 1},
	}

	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			resetUpdateVars(t)
			s := &updateServer{}
			s.start(t)
			setUpdateFlags(func() {
				updateIdleTimeout = tc.idleTimeout
				updateScaleThreshold = tc.scaleThreshold
			})

			if err := runUpdate(jsonOutputCmd("json"), []string{"ep-123"}); err != nil {
				t.Fatalf("expected boundary value to be accepted, got %v", err)
			}
			block, _ := s.patchBody[tc.block].(map[string]interface{})
			if got := block[tc.field]; got != tc.wantValue {
				t.Errorf("expected %s.%s %v, got %#v", tc.block, tc.field, tc.wantValue, s.patchBody)
			}
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetUpdateVars(t)
			setUpdateFlags(func() {
				updateIdleTimeout = tc.idleTimeout
				updateScaleThreshold = tc.scaleThreshold
			})

			// validation must fail before any api client is built, so no server here.
			err := runUpdate(jsonOutputCmd("json"), []string{"ep-123"})
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestRunUpdate_ModelReferences(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		for _, clear := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/clear=%t", format, clear), func(t *testing.T) {
				resetUpdateVars(t)

				savedRefs := []string{"https://huggingface.co/org/model:resolved-commit"}
				if clear {
					savedRefs = []string{}
				}
				var saveInput map[string]interface{}

				s := &updateServer{gql: func(query string, body map[string]interface{}) string {
					// rest v2 reports no templateId, so the saveEndpoint round
					// trip reads its config over graphql
					if strings.Contains(query, "EndpointConfig") {
						return `{"data":{"myself":{"endpoint":{"id":"ep-123","name":"my-endpoint",
							"templateId":"tpl-1","gpuIds":"ADA_24","idleTimeout":42,
							"scalerType":"REQUEST_COUNT","scalerValue":9,"workersMax":5,
							"flashBootType":"OFF"}}}}`
					}
					vars, _ := body["variables"].(map[string]interface{})
					saveInput, _ = vars["input"].(map[string]interface{})
					refs, _ := json.Marshal(savedRefs)
					return `{"data":{"saveEndpoint":{"id":"ep-123","name":"my-endpoint","modelReferences":` + string(refs) + `}}}`
				}}
				s.start(t)
				setUpdateFlags(func() {
					updateModelRefs = []string{"https://huggingface.co/org/model:main"}
					updateClearModels = clear
					if clear {
						updateModelRefs = nil
					}
				})

				stdout, _ := captureOutput(t, func() {
					if err := runUpdate(jsonOutputCmd(format), []string{"ep-123"}); err != nil {
						t.Errorf("unexpected error: %v", err)
					}
				})
				var result map[string]interface{}
				if err := yaml.Unmarshal([]byte(stdout), &result); err != nil {
					t.Fatal(err)
				}
				got, ok := result["modelReferences"].([]interface{})
				if !ok || len(got) != len(savedRefs) {
					t.Fatalf("expected explicit saved references %v, got %s", savedRefs, stdout)
				}
				if !clear && got[0] != savedRefs[0] {
					t.Fatalf("expected resolved reference, got %v", got)
				}
				// the rest of the output is the v2 read
				if result["id"] != "ep-123" || result["idleTimeout"] != 42 {
					t.Fatalf("endpoint fields lost: %s", stdout)
				}

				refs, _ := saveInput["modelReferences"].([]interface{})
				if (clear && len(refs) != 0) || (!clear && (len(refs) != 1 || refs[0] != "https://huggingface.co/org/model:main")) {
					t.Fatalf("expected modelReferences to contain the provided ref, got %#v", refs)
				}
				// existing config must be round-tripped, not reset to defaults.
				for field, want := range map[string]interface{}{
					"templateId": "tpl-1", "gpuIds": "ADA_24", "idleTimeout": float64(42),
					"scalerValue": float64(9), "workersMax": float64(5),
				} {
					if saveInput[field] != want {
						t.Errorf("%s not round-tripped: got %v, want %v", field, saveInput[field], want)
					}
				}
				if s.patchBody != nil {
					t.Errorf("a model-only update must not patch rest fields: %#v", s.patchBody)
				}
			})
		}
	}
}
