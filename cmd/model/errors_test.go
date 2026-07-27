package model

import (
	"encoding/json"
	"errors"
	"io"
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

func TestModelRepoError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{
			name:    "nil error is a no-op",
			err:     nil,
			wantErr: false,
		},
		{
			name:    "ErrModelRepoNotImplemented is recognized",
			err:     api.ErrModelRepoNotImplemented,
			wantErr: true,
		},
		{
			name:    "feature-not-enabled message is recognized",
			err:     errors.New("Model Repo feature is not enabled for this user"),
			wantErr: true,
		},
		{
			name:    "unrelated error is not recognized",
			err:     errors.New("some other failure"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got error
			// modelRepoError must not print: the handler returns the error and
			// the Execute sink emits the single JSON object. Printing here as
			// well would double-print (the bug CON-683 removed elsewhere), and
			// swallowing it made the process exit 0 on a real failure.
			stdout, stderr := captureStdStreams(t, func() {
				got = modelRepoError(tt.err)
			})

			if (got != nil) != tt.wantErr {
				t.Fatalf("modelRepoError() = %v, wantErr %v", got, tt.wantErr)
			}
			if stdout != "" {
				t.Fatalf("stdout must remain empty, got %q", stdout)
			}
			if stderr != "" {
				t.Fatalf("stderr must remain empty (the sink prints), got %q", stderr)
			}
		})
	}
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
			name:     "missing model-path is a not_found",
			setup:    func(t *testing.T) { addModelDirectoryPath = filepath.Join(t.TempDir(), "absent") },
			wantCode: "not_found",
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
