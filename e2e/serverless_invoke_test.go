//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"
)

// mockWorkerImage is Runpod's public mock serverless worker (~170MB, actively
// maintained). Its handler honours mock_return / mock_delay / mock_error, which
// is exactly what an invoke test needs: a deterministic result, a slow job and a
// failing job without building an image. runpod/serverless-hello-world also
// works but is 4.6GB, so it cold-starts far slower.
const mockWorkerImage = "runpod/mock-worker:latest"

// e2eInvokeWait is the --wait handed to the cli. A cold cpu worker spends ~90s
// pulling the image and booting before the handler runs, which is why the cli
// default (api.DefaultInvokeWait) is minutes rather than the 30s control-plane
// timeout.
const e2eInvokeWait = 6 * time.Minute

var (
	buildOnce   sync.Once
	builtBinary string
	buildErr    error
)

// invokeBinary builds the cli under test once per run into a temp dir.
//
// Deliberately not the ~/go/bin/runpodctl that runCLI in cli_test.go uses: that
// is a shared path, and installing over it would clobber whatever else is using
// it (including a concurrent test run from another worktree). These tests must
// exercise the binary, not internal/api, because the whole point of this ticket
// is command-level behaviour — exit codes and the stdout/stderr split.
func invokeBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := filepath.Abs("..")
		if err != nil {
			buildErr = err
			return
		}
		out := filepath.Join(t.TempDir(), "runpodctl-e2e")
		cmd := exec.Command("go", "build", "-o", out, ".")
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			buildErr = errors.New(string(output))
			return
		}
		builtBinary = out
	})
	if buildErr != nil {
		t.Fatalf("failed to build the cli: %v", buildErr)
	}
	return builtBinary
}

// cliResult is the full observable outcome of one cli invocation.
type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// errorObject is the flat error shape the cli emits on stderr.
type errorObject struct {
	Error  string `json:"error"`
	Code   string `json:"code"`
	Status int    `json:"status"`
}

// stderrError decodes the last line of stderr as the cli's error object. Progress
// notes share the stream, so only the final line is the error.
func (r cliResult) stderrError(t *testing.T) errorObject {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(r.stderr), "\n")
	var obj errorObject
	last := lines[len(lines)-1]
	if err := json.Unmarshal([]byte(last), &obj); err != nil {
		t.Fatalf("stderr does not end with a json error object: %q", r.stderr)
	}
	return obj
}

// stdoutJSON decodes stdout, asserting the cli emitted exactly one json document
// and nothing else.
func (r cliResult) stdoutJSON(t *testing.T) map[string]interface{} {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(r.stdout), &payload); err != nil {
		t.Fatalf("stdout is not a single json document: %q", r.stdout)
	}
	return payload
}

func runInvokeCLI(t *testing.T, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(invokeBinary(t), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := cliResult{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		result.exitCode = exitErr.ExitCode()
	default:
		t.Fatalf("failed to run the cli: %v", err)
	}
	return result
}

func TestE2E_ServerlessHealth(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	endpoints, err := client.ListEndpoints(nil)
	if err != nil {
		t.Fatalf("failed to list endpoints: %v", err)
	}
	if len(endpoints) == 0 {
		t.Skip("no endpoints on this account to check health for")
	}

	result := runInvokeCLI(t, "serverless", "health", endpoints[0].ID)
	if result.exitCode != 0 {
		t.Fatalf("exit = %d, want 0 (stderr %q)", result.exitCode, result.stderr)
	}
	health := result.stdoutJSON(t)
	for _, key := range []string{"jobs", "workers"} {
		if _, ok := health[key]; !ok {
			t.Errorf("health payload has no %q: %v", key, health)
		}
	}
	if strings.TrimSpace(result.stderr) != "" {
		t.Errorf("health is a plain read, stderr should be empty: %q", result.stderr)
	}
	t.Logf("health for %s: %s", endpoints[0].ID, strings.TrimSpace(result.stdout))
}

func TestE2E_ServerlessHealthNotFound(t *testing.T) {
	result := runInvokeCLI(t, "serverless", "health", "e2e-does-not-exist-1234")

	if result.exitCode == 0 {
		t.Fatalf("a bogus endpoint id must exit non-zero (stdout %q)", result.stdout)
	}
	// errors never reach stdout.
	if strings.TrimSpace(result.stdout) != "" {
		t.Errorf("stdout must stay empty on an error, got %q", result.stdout)
	}
	if obj := result.stderrError(t); obj.Code != "not_found" {
		t.Errorf("code = %q, want not_found (error %q)", obj.Code, obj.Error)
	}
}

func TestE2E_ServerlessStatusNotFound(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	endpoints, err := client.ListEndpoints(nil)
	if err != nil {
		t.Fatalf("failed to list endpoints: %v", err)
	}
	if len(endpoints) == 0 {
		t.Skip("no endpoints on this account")
	}

	result := runInvokeCLI(t, "serverless", "status", endpoints[0].ID, "e2e-no-such-job-1234")
	if result.exitCode == 0 {
		t.Fatal("a bogus job id must exit non-zero")
	}
	if obj := result.stderrError(t); obj.Code != "not_found" {
		t.Errorf("code = %q, want not_found (error %q)", obj.Code, obj.Error)
	}
}

func TestE2E_ServerlessRunRejectsBadInputLocally(t *testing.T) {
	// no api call should happen at all, so a bogus endpoint id is fine and free.
	tests := []struct {
		name string
		args []string
	}{
		{name: "malformed json", args: []string{"serverless", "run", "ep-does-not-matter", "--input", `{"a":`}},
		{name: "not an object", args: []string{"serverless", "run", "ep-does-not-matter", "--input", `[1,2,3]`}},
		{name: "no input flag", args: []string{"serverless", "run", "ep-does-not-matter"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runInvokeCLI(t, tt.args...)
			if result.exitCode == 0 {
				t.Fatalf("expected a non-zero exit, got stdout %q", result.stdout)
			}
			if obj := result.stderrError(t); obj.Code != "usage_error" {
				t.Errorf("code = %q, want usage_error (error %q)", obj.Code, obj.Error)
			}
			if strings.TrimSpace(result.stdout) != "" {
				t.Errorf("stdout must stay empty, got %q", result.stdout)
			}
		})
	}
}

// TestE2E_ServerlessInvokeLifecycle creates a throwaway cpu endpoint on the
// public mock worker, drives the cli against it and deletes everything it made.
// workersMin is 0, so the endpoint costs nothing while idle and the invocations
// cost a fraction of a cent.
func TestE2E_ServerlessInvokeLifecycle(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	suffix := time.Now().Format("20060102150405")

	template, err := client.CreateTemplate(&api.TemplateCreateRequest{
		Name:              "e2e-invoke-" + suffix,
		ImageName:         mockWorkerImage,
		IsServerless:      true,
		ContainerDiskInGb: 10,
	})
	if err != nil {
		t.Fatalf("failed to create template: %v", err)
	}
	t.Cleanup(func() {
		if err := client.DeleteTemplate(template.ID); err != nil {
			t.Errorf("failed to delete template %s: %v", template.ID, err)
			return
		}
		t.Logf("deleted template %s", template.ID)
	})

	endpoint, err := client.CreateEndpointGQL(&api.EndpointCreateGQLInput{
		Name:        "e2e-invoke-" + suffix,
		TemplateID:  template.ID,
		InstanceIDs: []string{"cpu3g-4-16"},
		WorkersMin:  0,
		WorkersMax:  1,
	})
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}
	t.Cleanup(func() {
		if err := client.DeleteEndpoint(endpoint.ID); err != nil {
			t.Errorf("failed to delete endpoint %s: %v", endpoint.ID, err)
			return
		}
		t.Logf("deleted endpoint %s", endpoint.ID)
	})
	t.Logf("created endpoint %s on %s", endpoint.ID, mockWorkerImage)

	wait := e2eInvokeWait.String()

	// the first invocation is the cold start, so it carries the long wait.
	t.Run("run waits for the result", func(t *testing.T) {
		result := runInvokeCLI(t, "serverless", "run", endpoint.ID,
			"--input", `{"mock_return":"con-688 run"}`, "--wait", wait)
		if result.exitCode != 0 {
			t.Fatalf("exit = %d, want 0 (stderr %q)", result.exitCode, result.stderr)
		}
		job := result.stdoutJSON(t)
		if job["status"] != api.JobStatusCompleted {
			t.Fatalf("status = %v, want COMPLETED (payload %v)", job["status"], job)
		}
		if job["output"] != "con-688 run" {
			t.Errorf("output = %v, want the handler payload echoed back", job["output"])
		}
		// progress notes belong on stderr, never mixed into the payload.
		if strings.Contains(result.stdout, "waiting for job") {
			t.Errorf("progress notes leaked into stdout: %q", result.stdout)
		}
		t.Logf("job %v completed", job["id"])
	})

	t.Run("no-wait returns the job id then status follows it", func(t *testing.T) {
		submitted := runInvokeCLI(t, "serverless", "run", endpoint.ID,
			"--input", `{"mock_return":"con-688 no-wait"}`, "--no-wait")
		if submitted.exitCode != 0 {
			t.Fatalf("exit = %d, want 0 (stderr %q)", submitted.exitCode, submitted.stderr)
		}
		job := submitted.stdoutJSON(t)
		jobID, _ := job["id"].(string)
		if jobID == "" {
			t.Fatalf("--no-wait must return a job id, got %v", job)
		}
		// the follow-up command must be spelled out for the caller.
		if !strings.Contains(submitted.stderr, "serverless status "+endpoint.ID+" "+jobID) {
			t.Errorf("expected the follow-up command on stderr, got %q", submitted.stderr)
		}

		followed := runInvokeCLI(t, "serverless", "status", endpoint.ID, jobID, "--wait", wait)
		if followed.exitCode != 0 {
			t.Fatalf("exit = %d, want 0 (stderr %q)", followed.exitCode, followed.stderr)
		}
		if status := followed.stdoutJSON(t)["status"]; status != api.JobStatusCompleted {
			t.Fatalf("status = %v, want COMPLETED", status)
		}
		t.Logf("job %s completed via status", jobID)
	})

	t.Run("input from stdin", func(t *testing.T) {
		cmd := exec.Command(invokeBinary(t), "serverless", "run", endpoint.ID, "--input", "-", "--wait", wait)
		cmd.Stdin = strings.NewReader(`{"mock_return":"con-688 stdin"}`)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("stdin payload failed: %v (stderr %q)", err, stderr.String())
		}
		result := cliResult{stdout: stdout.String(), stderr: stderr.String()}
		if output := result.stdoutJSON(t)["output"]; output != "con-688 stdin" {
			t.Errorf("output = %v, want the stdin payload echoed back", output)
		}
	})

	t.Run("failed job exits 1 with the payload on stdout", func(t *testing.T) {
		result := runInvokeCLI(t, "serverless", "run", endpoint.ID,
			"--input", `{"mock_error":true}`, "--wait", wait)
		if result.exitCode != 1 {
			t.Fatalf("exit = %d, want 1 (stderr %q)", result.exitCode, result.stderr)
		}
		job := result.stdoutJSON(t)
		if job["status"] != api.JobStatusFailed {
			t.Fatalf("status = %v, want FAILED (payload %v)", job["status"], job)
		}
		// the worker's own error is the useful artifact and stays on stdout...
		if _, ok := job["error"]; !ok {
			t.Errorf("expected the worker error in the payload, got %v", job)
		}
		// ...while the coded error object goes to stderr.
		if obj := result.stderrError(t); obj.Code != "job_failed" {
			t.Errorf("code = %q, want job_failed (error %q)", obj.Code, obj.Error)
		}
	})

	t.Run("wait budget too small is an actionable timeout", func(t *testing.T) {
		// the handler sleeps well past the budget, so the cli must stop waiting and
		// hand back a job that is still running plus the command to poll it.
		result := runInvokeCLI(t, "serverless", "run", endpoint.ID,
			"--input", `{"mock_delay":25}`, "--wait", "5s")
		if result.exitCode != 1 {
			t.Fatalf("exit = %d, want 1 (stderr %q)", result.exitCode, result.stderr)
		}
		job := result.stdoutJSON(t)
		jobID, _ := job["id"].(string)
		if jobID == "" {
			t.Fatalf("the last known payload must carry the job id, got %v", job)
		}
		obj := result.stderrError(t)
		if obj.Code != "timeout" {
			t.Fatalf("code = %q, want timeout (error %q)", obj.Code, obj.Error)
		}
		if !strings.Contains(obj.Error, "serverless status "+endpoint.ID+" "+jobID) {
			t.Errorf("timeout error is not actionable: %q", obj.Error)
		}

		// follow the advice verbatim: it must work.
		followed := runInvokeCLI(t, "serverless", "status", endpoint.ID, jobID, "--wait", wait)
		if followed.exitCode != 0 {
			t.Fatalf("following the timeout advice failed: exit %d (stderr %q)", followed.exitCode, followed.stderr)
		}
		if status := followed.stdoutJSON(t)["status"]; status != api.JobStatusCompleted {
			t.Errorf("status = %v, want COMPLETED", status)
		}
		t.Logf("timed-out job %s followed to completion", jobID)
	})
}
