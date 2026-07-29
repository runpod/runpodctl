package serverless

import (
	"strings"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"
)

func TestStatusCmd_Flags(t *testing.T) {
	if statusCmd.Flags().Lookup("wait") == nil {
		t.Fatal("expected --wait flag")
	}
	if got := statusCmd.Flags().Lookup("wait").DefValue; got != "0s" {
		t.Errorf("--wait default = %q, want 0s (a single check)", got)
	}
}

// snapshotStatusFlags restores the status globals after a test.
func snapshotStatusFlags(t *testing.T) {
	t.Helper()
	old := statusWait
	t.Cleanup(func() { statusWait = old })
	statusWait = 0
}

func TestRunStatus_SingleCheck(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantCode string
	}{
		{name: "queued job is a successful query", payload: `{"id":"job-1","status":"IN_QUEUE"}`},
		{name: "running job is a successful query", payload: `{"id":"job-1","status":"IN_PROGRESS"}`},
		{name: "completed job", payload: `{"id":"job-1","status":"COMPLETED","output":"ok"}`},
		{name: "failed job", payload: `{"id":"job-1","status":"FAILED","error":"boom"}`, wantCode: "job_failed"},
		{name: "timed out job", payload: `{"id":"job-1","status":"TIMED_OUT"}`, wantCode: "job_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshotStatusFlags(t)
			client := installMockInvokeClient(t, &mockInvokeClient{statusSteps: []jobStep{{job: mustJob(tt.payload)}}})

			var err error
			stdout, _ := captureOutput(t, func() {
				err = runStatus(mockInvokeCommand(""), []string{"ep-1", "job-1"})
			})
			if code := errorCode(t, err); code != tt.wantCode {
				t.Fatalf("code = %q, want %q (err %v)", code, tt.wantCode, err)
			}
			if client.statusCalls != 1 {
				t.Errorf("status calls = %d, want 1 without --wait", client.statusCalls)
			}
			// the payload is the artifact in every case, including a failed job.
			if !strings.Contains(stdout, `"id": "job-1"`) {
				t.Errorf("job payload missing from stdout: %q", stdout)
			}
		})
	}
}

func TestRunStatus_WaitPollsUntilTerminal(t *testing.T) {
	snapshotStatusFlags(t)
	fastPolling(t)
	statusWait = 5 * time.Second
	client := installMockInvokeClient(t, &mockInvokeClient{statusSteps: []jobStep{
		{job: mustJob(`{"id":"job-1","status":"IN_PROGRESS"}`)},
		{job: mustJob(`{"id":"job-1","status":"COMPLETED","output":"ok"}`)},
	}})

	var err error
	stdout, stderr := captureOutput(t, func() {
		err = runStatus(mockInvokeCommand(""), []string{"ep-1", "job-1"})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.statusCalls != 2 {
		t.Errorf("status calls = %d, want 2 (initial check plus one poll)", client.statusCalls)
	}
	if !strings.Contains(stdout, `"output": "ok"`) {
		t.Errorf("final payload missing from stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "job job-1: COMPLETED") {
		t.Errorf("expected a completion note on stderr, got %q", stderr)
	}
}

func TestRunStatus_WaitBudgetExhausted(t *testing.T) {
	snapshotStatusFlags(t)
	fastPolling(t)
	statusWait = 20 * time.Millisecond
	installMockInvokeClient(t, &mockInvokeClient{statusSteps: []jobStep{{job: mustJob(`{"id":"job-1","status":"IN_PROGRESS"}`)}}})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runStatus(mockInvokeCommand(""), []string{"ep-1", "job-1"})
	})
	if code := errorCode(t, err); code != "timeout" {
		t.Fatalf("code = %q, want timeout (err %v)", code, err)
	}
	if !strings.Contains(stdout, `"status": "IN_PROGRESS"`) {
		t.Errorf("last known payload missing from stdout: %q", stdout)
	}
}

func TestRunStatus_APIErrorKeepsCode(t *testing.T) {
	snapshotStatusFlags(t)
	installMockInvokeClient(t, &mockInvokeClient{statusSteps: []jobStep{
		{err: &api.APIError{Message: "job not found", Status: 404}},
	}})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runStatus(mockInvokeCommand(""), []string{"ep-1", "missing"})
	})
	if code := errorCode(t, err); code != "not_found" {
		t.Fatalf("code = %q, want not_found (err %v)", code, err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("errors must not reach stdout, got %q", stdout)
	}
}

func TestRunStatus_RejectsNegativeWait(t *testing.T) {
	snapshotStatusFlags(t)
	statusWait = -time.Second

	var err error
	_, _ = captureOutput(t, func() {
		err = runStatus(mockInvokeCommand(""), []string{"ep-1", "job-1"})
	})
	if code := errorCode(t, err); code != "usage_error" {
		t.Fatalf("code = %q, want usage_error (err %v)", code, err)
	}
}
