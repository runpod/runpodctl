package model

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/runpod/runpodctl/api"
	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/runpod/runpodctl/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// captureStdStreams runs fn with os.Stdout and os.Stderr replaced by pipes and
// returns whatever each stream received. It exists to assert that informational
// and error output goes to the correct stream — a regression class CLAUDE.md
// explicitly calls out (legacy commands losing stderr ⇒ corrupts stdout for
// JSON-consuming agents).
func captureStdStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	origStdout, origStderr := os.Stdout, os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe stdout: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe stderr: %v", err)
	}
	os.Stdout, os.Stderr = stdoutW, stderrW

	var (
		wg                   sync.WaitGroup
		stdoutBuf, stderrBuf strings.Builder
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stdoutBuf, stdoutR)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stderrBuf, stderrR)
	}()

	defer func() {
		os.Stdout, os.Stderr = origStdout, origStderr
	}()

	fn()

	_ = stdoutW.Close()
	_ = stderrW.Close()
	wg.Wait()
	_ = stdoutR.Close()
	_ = stderrR.Close()

	return stdoutBuf.String(), stderrBuf.String()
}

// TestRunAddModelValidationErrorsCarryACodeAndPrintNothing pins the CON-683
// contract on `model add`, which used to reach cobra.CheckErr on these paths:
// plaintext "Error: …" on stderr plus os.Exit(1), bypassing the json sink. So a
// single command emitted json+code on some failures and plaintext on others and
// an agent could rely on neither shape.
func TestRunAddModelValidationErrorsCarryACodeAndPrintNothing(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "weights.bin")
	if err := os.WriteFile(notADir, []byte("model"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	emptyDir := t.TempDir()

	tests := []struct {
		name     string
		setup    func(t *testing.T)
		wantCode string
		wantMsg  string
	}{
		{
			// a missing *local* path is cli_error, not not_found: not_found is
			// reserved for resources the api does not have, so an agent can trust
			// it to mean "server-side", not "you typed the path wrong".
			name:     "missing model-path is a cli_error",
			setup:    func(t *testing.T) { addModelDirectoryPath = filepath.Join(t.TempDir(), "absent") },
			wantCode: "cli_error",
			wantMsg:  "does not exist",
		},
		{
			name:     "model-path pointing at a file",
			setup:    func(t *testing.T) { addModelDirectoryPath = notADir },
			wantCode: "cli_error",
			wantMsg:  "must be a directory",
		},
		{
			name:     "model-path with no files",
			setup:    func(t *testing.T) { addModelDirectoryPath = emptyDir },
			wantCode: "cli_error",
			wantMsg:  "does not contain any files to upload",
		},
		{
			name:     "wait-for-hash without model-path",
			setup:    func(t *testing.T) { addModelWaitForHash = true },
			wantCode: "cli_error",
			wantMsg:  "--wait-for-hash requires --model-path",
		},
		{
			name: "create-upload without file-name",
			setup: func(t *testing.T) {
				addModelCreateUpload = true
				old := addModelToRepo
				t.Cleanup(func() { addModelToRepo = old })
				addModelToRepo = func(*api.AddModelToRepoInput) (*api.Model, error) {
					return &api.Model{ID: "model-id"}, nil
				}
			},
			wantCode: "cli_error",
			wantMsg:  "file-name is required when creating an upload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAddModelGlobals(t)
			addModelName = "test-model"
			tt.setup(t)

			cmd := newTestAddModelCommand()
			var runErr error
			stdout, stderr := captureStdStreams(t, func() {
				runErr = runAddModel(cmd, nil)
			})

			if runErr == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(runErr.Error(), tt.wantMsg) {
				t.Fatalf("expected error containing %q, got %v", tt.wantMsg, runErr)
			}
			if stdout != "" {
				t.Fatalf("stdout must stay empty on failure, got %q", stdout)
			}
			if stderr != "" {
				t.Fatalf("the command must not print the error; the sink does. got %q", stderr)
			}

			sinkStdout, sinkStderr := captureStdStreams(t, func() {
				output.Error(runErr)
			})
			if sinkStdout != "" {
				t.Fatalf("the sink must not write to stdout, got %q", sinkStdout)
			}
			if strings.Count(strings.TrimSpace(sinkStderr), "\n") != 0 {
				t.Fatalf("expected exactly one json object on stderr, got %q", sinkStderr)
			}
			var obj struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			if err := json.Unmarshal([]byte(sinkStderr), &obj); err != nil {
				t.Fatalf("stderr must be a json error object: %v (%q)", err, sinkStderr)
			}
			if obj.Code != tt.wantCode {
				t.Fatalf("expected code %q, got %q (%q)", tt.wantCode, obj.Code, sinkStderr)
			}
		})
	}
}

// TestLegacyCommandKeepsStdoutCleanWithoutAPIKey pins the deliberate exception
// recorded under "pitfalls" in AGENTS.md. api/query.go — which backs the legacy
// create/get/remove/start/stop pod commands as well as model — used to print
// "API key not found" to *stdout* while also returning the error, so anything
// piping runpodctl output into a json parser got that string mixed into the data
// stream. stdout is the data channel; the error now travels back to the Execute
// sink, which emits one flat json object with code no_credentials on stderr.
//
// Uses the hidden legacy alias (`runpodctl get models`) on purpose: it is the
// pre-restructure surface AGENTS.md tells you not to change, so this is the path
// where a regression would be least visible.
func TestLegacyCommandKeepsStdoutCleanWithoutAPIKey(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	// empty env value plus a reset config means configenv.APIKey() is "", so
	// api.Query short-circuits before any network call.
	t.Setenv(configenv.APIKeyEnv, "")

	cmd := &cobra.Command{Use: "models"}
	cmd.Flags().String("output", "json", "")

	var runErr error
	stdout, stderr := captureStdStreams(t, func() {
		runErr = GetModelsCmd.RunE(cmd, nil)
	})

	if runErr == nil {
		t.Fatal("expected an error with no api key configured")
	}
	if stdout != "" {
		t.Fatalf("stdout must stay a clean data channel, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("the command must not print the error; the sink does. got %q", stderr)
	}

	var apiErr *internalapi.APIError
	if !errors.As(runErr, &apiErr) || apiErr.ErrorCode() != "no_credentials" {
		t.Fatalf("expected a no_credentials api error, got %#v", runErr)
	}

	sinkStdout, sinkStderr := captureStdStreams(t, func() {
		output.Error(runErr)
	})
	if sinkStdout != "" {
		t.Fatalf("the sink must not write to stdout, got %q", sinkStdout)
	}
	if strings.Count(strings.TrimSpace(sinkStderr), "\n") != 0 {
		t.Fatalf("expected exactly one json object on stderr, got %q", sinkStderr)
	}
	var obj struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal([]byte(sinkStderr), &obj); err != nil {
		t.Fatalf("stderr must be a json error object: %v (%q)", err, sinkStderr)
	}
	if obj.Code != "no_credentials" {
		t.Fatalf("expected code no_credentials, got %q", obj.Code)
	}
	if obj.Error == "" {
		t.Fatal("expected an error message on the json object")
	}
}

// TestModelCommandsUseRunE pins the exit-code contract: a model command that
// fails must return its error so cobra exits non-zero. They previously used
// Run: with a swallowed error, so a real Model Repo failure exited 0 and any
// script doing `runpodctl model add … && echo ok` printed ok.
func TestModelCommandsUseRunE(t *testing.T) {
	for _, c := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{"model list", listCmd},
		{"model list (legacy alias)", GetModelsCmd},
		{"model add", addCmd},
		{"model add (legacy alias)", AddModelToRepoCmd},
		{"model remove", removeCmd},
		{"model remove (legacy alias)", RemoveModelCmd},
	} {
		if c.cmd == nil {
			t.Errorf("%s: command is nil — the test can no longer see it", c.name)
			continue
		}
		if c.cmd.RunE == nil {
			t.Errorf("%s: RunE is nil — errors cannot propagate to the exit code", c.name)
		}
		if c.cmd.Run != nil {
			t.Errorf("%s: Run is set; use RunE so errors reach the sink", c.name)
		}
	}
}

// TestRunAddModelPropagatesErrors covers what the validation table cannot: that
// a failure from each stubbed dependency actually reaches the caller. An audit
// mutated ten of these paths to `return nil` / `_ =` and every existing test
// stayed green, so each case here is a mutation the suite previously missed.
func TestRunAddModelPropagatesErrors(t *testing.T) {
	boom := errors.New("boom")

	tests := []struct {
		name  string
		setup func(t *testing.T)
	}{
		{
			name: "addModelToRepo failure",
			setup: func(t *testing.T) {
				addModelName = "m"
				addModelToRepo = func(*api.AddModelToRepoInput) (*api.Model, error) { return nil, boom }
			},
		},
		{
			name: "createModelRepoUpload failure on the create-upload branch",
			setup: func(t *testing.T) {
				addModelName = "m"
				addModelCreateUpload = true
				addModelFileName = "f.bin"
				addModelFileSize = "10"
				addModelToRepo = func(*api.AddModelToRepoInput) (*api.Model, error) {
					return &api.Model{ID: "model-id"}, nil
				}
				createModelRepoUpload = func(*api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
					return nil, boom
				}
			},
		},
		{
			name: "nil upload session is rejected",
			setup: func(t *testing.T) {
				addModelName = "m"
				addModelCreateUpload = true
				addModelFileName = "f.bin"
				addModelFileSize = "10"
				addModelToRepo = func(*api.AddModelToRepoInput) (*api.Model, error) {
					return &api.Model{ID: "model-id"}, nil
				}
				createModelRepoUpload = func(*api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
					return &api.ModelRepoMutationResult{}, nil // Upload == nil
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAddModelGlobals(t)
			oldAdd, oldCreate := addModelToRepo, createModelRepoUpload
			t.Cleanup(func() { addModelToRepo, createModelRepoUpload = oldAdd, oldCreate })
			tt.setup(t)

			var err error
			stdout, stderr := captureStdStreams(t, func() {
				err = runAddModel(newTestAddModelCommand(), nil)
			})

			if err == nil {
				t.Fatal("expected an error to propagate to the caller; a nil here means the cli exits 0 on a failure")
			}
			if stdout != "" {
				t.Errorf("stdout must stay empty on failure, got %q", stdout)
			}
			if stderr != "" {
				t.Errorf("the command must not print the error itself, got %q", stderr)
			}
		})
	}
}

// TestCreateUploadValidationRunsBeforeCreatingTheModel pins the ordering fix: a
// missing --file-name must fail BEFORE addModelToRepo is called, otherwise the
// model is created server-side and the cli reports a plain validation error, so
// an agent retrying creates a duplicate.
func TestCreateUploadValidationRunsBeforeCreatingTheModel(t *testing.T) {
	for _, missing := range []string{"file-name", "file-size"} {
		t.Run(missing, func(t *testing.T) {
			resetAddModelGlobals(t)
			old := addModelToRepo
			t.Cleanup(func() { addModelToRepo = old })

			called := false
			addModelToRepo = func(*api.AddModelToRepoInput) (*api.Model, error) {
				called = true
				return &api.Model{ID: "model-id"}, nil
			}

			addModelName = "m"
			addModelCreateUpload = true
			if missing == "file-size" {
				addModelFileName = "f.bin"
			}

			err := runAddModel(newTestAddModelCommand(), nil)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !strings.Contains(err.Error(), missing+" is required") {
				t.Errorf("error = %v, want it to name %s", err, missing)
			}
			if called {
				t.Error("addModelToRepo was called before validation — a model would be created server-side and then orphaned")
			}
		})
	}
}

// newModelRepoDisabledServer stands in for the real backend's response when
// Model Repo is unavailable, disabled, or inaccessible for the caller: the
// resolver throws before returning data (see assertModelRepoFeatureEnabled in
// node/graphql/schema/modelRepo.ts), which graphql reports as a 200 response
// with a top-level `errors` array and no successful data.
func newModelRepoDisabledServer(t *testing.T) string {
	t.Helper()
	const disabledMessage = "Model Repo feature is not enabled for this user"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": nil,
			"errors": []map[string]interface{}{
				{"message": disabledMessage, "extensions": map[string]interface{}{"code": "RUNPOD"}},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// TestModelCommandsExitNonZeroWhenModelRepoAccessDenied pins the STO-357
// contract end to end for each `model` command: when Model Repo is
// unavailable, disabled, or inaccessible, the command must return a non-nil
// error (so cobra/cmd/root.go exit 1), the command itself must stay silent on
// both streams (the sink owns printing), and the sink must emit a single
// stable-coded json error object on stderr -- "graphql_error", not the generic
// "cli_error" fallback -- so automation can reliably tell a Model Repo access
// failure apart from an unrelated local cli mistake.
func TestModelCommandsExitNonZeroWhenModelRepoAccessDenied(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv(configenv.APIKeyEnv, "test-key")
	t.Setenv(configenv.GraphQLURLEnv, newModelRepoDisabledServer(t))

	assertGraphQLErrorContract := func(t *testing.T, runErr error) {
		t.Helper()
		if runErr == nil {
			t.Fatal("expected an error when Model Repo access is denied; a nil error means the cli exits 0")
		}

		var gqlErr *internalapi.GraphQLError
		if !errors.As(runErr, &gqlErr) {
			t.Fatalf("expected a *internalapi.GraphQLError so the code is stable, got %#v (%v)", runErr, runErr)
		}
		if gqlErr.ErrorCode() != "graphql_error" {
			t.Fatalf("expected code graphql_error, got %q", gqlErr.ErrorCode())
		}

		sinkStdout, sinkStderr := captureStdStreams(t, func() {
			output.Error(runErr)
		})
		if sinkStdout != "" {
			t.Fatalf("the sink must not write to stdout, got %q", sinkStdout)
		}
		var obj struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if err := json.Unmarshal([]byte(sinkStderr), &obj); err != nil {
			t.Fatalf("stderr must be a json error object: %v (%q)", err, sinkStderr)
		}
		if obj.Code != "graphql_error" {
			t.Fatalf("expected sink code graphql_error, got %q (%q)", obj.Code, sinkStderr)
		}
		if obj.Error == "" {
			t.Fatal("expected a non-empty message on the json object")
		}
	}

	t.Run("model list", func(t *testing.T) {
		cmd := &cobra.Command{Use: "list"}
		cmd.Flags().String("output", "json", "")

		var runErr error
		stdout, stderr := captureStdStreams(t, func() {
			runErr = runModelList(cmd, nil)
		})
		if stdout != "" || stderr != "" {
			t.Fatalf("the command must not print anything itself, got stdout=%q stderr=%q", stdout, stderr)
		}
		assertGraphQLErrorContract(t, runErr)
	})

	t.Run("model add", func(t *testing.T) {
		resetAddModelGlobals(t)
		addModelName = "test-model"

		var runErr error
		stdout, stderr := captureStdStreams(t, func() {
			runErr = runAddModel(newTestAddModelCommand(), nil)
		})
		if stdout != "" || stderr != "" {
			t.Fatalf("the command must not print anything itself, got stdout=%q stderr=%q", stdout, stderr)
		}
		assertGraphQLErrorContract(t, runErr)
	})

	t.Run("model remove", func(t *testing.T) {
		originalOwner, originalName := removeOwner, removeName
		originalHash, originalVersion := removeHash, removeVersion
		t.Cleanup(func() {
			removeOwner, removeName = originalOwner, originalName
			removeHash, removeVersion = originalHash, originalVersion
		})
		removeOwner, removeName = "owner", "name"
		removeHash, removeVersion = "", ""

		cmd := &cobra.Command{Use: "remove"}
		cmd.Flags().String("output", "json", "")

		var runErr error
		stdout, stderr := captureStdStreams(t, func() {
			runErr = runRemoveModel(cmd, nil)
		})
		if stdout != "" || stderr != "" {
			t.Fatalf("the command must not print anything itself, got stdout=%q stderr=%q", stdout, stderr)
		}
		assertGraphQLErrorContract(t, runErr)
	})
}
