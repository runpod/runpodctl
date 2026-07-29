package serverless

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"
)

func TestRunCmd_Flags(t *testing.T) {
	flags := runCmd.Flags()

	for _, name := range []string{"input", "input-file", "async", "no-wait", "wait"} {
		if flags.Lookup(name) == nil {
			t.Errorf("expected --%s flag", name)
		}
	}
	if got := flags.Lookup("wait").DefValue; got != api.DefaultInvokeWait.String() {
		t.Errorf("--wait default = %q, want %q", got, api.DefaultInvokeWait.String())
	}
}

// snapshotRunFlags restores the run globals after a test and sets a known
// baseline matching the flag defaults.
func snapshotRunFlags(t *testing.T) {
	t.Helper()
	oldInput, oldFile, oldAsync, oldNoWait, oldWait := runInput, runInputFile, runAsync, runNoWait, runWait
	t.Cleanup(func() {
		runInput, runInputFile, runAsync, runNoWait, runWait = oldInput, oldFile, oldAsync, oldNoWait, oldWait
	})
	runInput, runInputFile = `{"prompt":"hi"}`, ""
	runAsync, runNoWait = false, false
	runWait = api.DefaultInvokeWait
}

func TestResolveJobInput(t *testing.T) {
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(payloadPath, []byte("  {\"prompt\":\"from file\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		inline   string
		file     string
		stdin    string
		want     string
		wantErr  string
		wantCode string
		wantNote string
	}{
		{name: "inline object", inline: `{"prompt":"hi"}`, want: `{"prompt":"hi"}`},
		{name: "inline trims whitespace", inline: "  {\"a\":1}\n", want: `{"a":1}`},
		{name: "inline array", inline: `[1,2]`, want: `[1,2]`},
		{name: "inline empty object", inline: `{}`, want: `{}`},
		{name: "from file", file: payloadPath, want: `{"prompt":"from file"}`},
		{name: "stdin via --input dash", inline: "-", stdin: `{"prompt":"stdin"}`, want: `{"prompt":"stdin"}`},
		{name: "stdin via --input-file dash", file: "-", stdin: `{"prompt":"stdin"}`, want: `{"prompt":"stdin"}`},
		{
			name: "double wrapped payload is flagged", inline: `{"input":{"prompt":"hi"}}`,
			want:     `{"input":{"prompt":"hi"}}`,
			wantNote: `it is sent as {"input": <payload>}`,
		},
		{
			name: "malformed json", inline: `{"prompt":`,
			wantErr: "payload from --input is not valid json", wantCode: "usage_error",
		},
		{
			name: "trailing garbage", inline: `{"a":1} oops`,
			wantErr: "payload from --input is not valid json", wantCode: "usage_error",
		},
		{
			name: "empty stdin", inline: "-", stdin: "   \n",
			wantErr: "payload from stdin is empty", wantCode: "usage_error",
		},
		{
			name: "both flags", inline: `{}`, file: payloadPath,
			wantErr: "--input and --input-file are mutually exclusive", wantCode: "usage_error",
		},
		{
			name:    "neither flag",
			wantErr: "one of --input or --input-file is required", wantCode: "usage_error",
		},
		{
			name: "missing file", file: filepath.Join(dir, "nope.json"),
			wantErr: "failed to read --input-file", wantCode: "cli_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				got     string
				gotErr  error
				gotNote string
			)
			_, gotNote = captureOutput(t, func() {
				raw, err := resolveJobInput(strings.NewReader(tt.stdin), tt.inline, tt.file)
				got, gotErr = string(raw), err
			})

			if tt.wantErr != "" {
				if gotErr == nil || !strings.Contains(gotErr.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", gotErr, tt.wantErr)
				}
				if code := errorCode(t, gotErr); code != tt.wantCode {
					t.Errorf("code = %q, want %q", code, tt.wantCode)
				}
				return
			}
			if gotErr != nil {
				t.Fatalf("unexpected error: %v", gotErr)
			}
			if got != tt.want {
				t.Errorf("payload = %q, want %q", got, tt.want)
			}
			if tt.wantNote != "" && !strings.Contains(gotNote, tt.wantNote) {
				t.Errorf("expected a stderr note containing %q, got %q", tt.wantNote, gotNote)
			}
			if tt.wantNote == "" && strings.Contains(gotNote, "note:") {
				t.Errorf("unexpected note on stderr: %q", gotNote)
			}
		})
	}
}

func TestRunRun_RunSyncCompleted(t *testing.T) {
	snapshotRunFlags(t)
	client := installMockInvokeClient(t, &mockInvokeClient{
		runSyncStep: jobStep{job: mustJob(`{"id":"job-1","status":"COMPLETED","output":{"echo":"hi"},"executionTime":120}`)},
	})

	var err error
	stdout, stderr := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.runSyncCalls != 1 || client.runCalls != 0 || client.statusCalls != 0 {
		t.Errorf("calls: runsync=%d run=%d status=%d, want 1/0/0", client.runSyncCalls, client.runCalls, client.statusCalls)
	}
	if string(client.lastInput) != `{"prompt":"hi"}` {
		t.Errorf("input sent = %q, want the raw handler payload", client.lastInput)
	}
	if !strings.Contains(stdout, `"echo": "hi"`) || !strings.Contains(stdout, `"executionTime": 120`) {
		t.Errorf("job payload missing from stdout: %q", stdout)
	}
	if stderr != "" {
		t.Errorf("a job that finishes on the first response needs no progress notes, got %q", stderr)
	}
}

func TestRunRun_RunSyncFallsBackToPolling(t *testing.T) {
	snapshotRunFlags(t)
	fastPolling(t)
	client := installMockInvokeClient(t, &mockInvokeClient{
		// this is what /runsync answers when the job outlives the ~90s the invoke
		// api holds the connection for.
		runSyncStep: jobStep{job: mustJob(`{"id":"job-1","status":"IN_PROGRESS"}`)},
		statusSteps: []jobStep{
			{job: mustJob(`{"id":"job-1","status":"IN_PROGRESS"}`)},
			{job: mustJob(`{"id":"job-1","status":"COMPLETED","output":"done"}`)},
		},
	})

	var err error
	stdout, stderr := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.statusCalls != 2 {
		t.Errorf("status calls = %d, want 2", client.statusCalls)
	}
	if !strings.Contains(stdout, `"output": "done"`) {
		t.Errorf("final payload missing from stdout: %q", stdout)
	}
	// progress belongs on stderr only.
	if !strings.Contains(stderr, "waiting for job job-1") {
		t.Errorf("expected progress notes on stderr, got %q", stderr)
	}
	if strings.Contains(stdout, "waiting for job") {
		t.Errorf("progress notes leaked into stdout: %q", stdout)
	}
}

func TestRunRun_FailedJobPrintsPayloadAndExitsNonZero(t *testing.T) {
	snapshotRunFlags(t)
	installMockInvokeClient(t, &mockInvokeClient{
		runSyncStep: jobStep{job: mustJob(`{"id":"job-1","status":"FAILED","error":"handler blew up"}`)},
	})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "job_failed" {
		t.Fatalf("code = %q, want job_failed (err %v)", code, err)
	}
	// the worker's error is the useful artifact and must still reach stdout.
	if !strings.Contains(stdout, "handler blew up") {
		t.Errorf("worker error missing from stdout: %q", stdout)
	}
}

func TestRunRun_AsyncSubmitsAndPolls(t *testing.T) {
	snapshotRunFlags(t)
	fastPolling(t)
	runAsync = true
	client := installMockInvokeClient(t, &mockInvokeClient{
		runStep:     jobStep{job: mustJob(`{"id":"job-2","status":"IN_QUEUE"}`)},
		statusSteps: []jobStep{{job: mustJob(`{"id":"job-2","status":"COMPLETED","output":"ok"}`)}},
	})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.runCalls != 1 || client.runSyncCalls != 0 {
		t.Errorf("--async must submit on /run: run=%d runsync=%d", client.runCalls, client.runSyncCalls)
	}
	if client.statusCalls != 1 {
		t.Errorf("status calls = %d, want 1", client.statusCalls)
	}
	if !strings.Contains(stdout, `"output": "ok"`) {
		t.Errorf("final payload missing from stdout: %q", stdout)
	}
}

func TestRunRun_NoWaitReturnsJobIDImmediately(t *testing.T) {
	snapshotRunFlags(t)
	runNoWait = true
	client := installMockInvokeClient(t, &mockInvokeClient{
		runStep: jobStep{job: mustJob(`{"id":"job-3","status":"IN_QUEUE"}`)},
	})

	var err error
	stdout, stderr := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if err != nil {
		t.Fatalf("--no-wait must succeed on submission: %v", err)
	}
	if client.runCalls != 1 || client.runSyncCalls != 0 || client.statusCalls != 0 {
		t.Errorf("calls: run=%d runsync=%d status=%d, want 1/0/0", client.runCalls, client.runSyncCalls, client.statusCalls)
	}
	if !strings.Contains(stdout, `"id": "job-3"`) || !strings.Contains(stdout, `"status": "IN_QUEUE"`) {
		t.Errorf("queued job missing from stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "runpodctl serverless status ep-1 job-3") {
		t.Errorf("expected the follow-up command on stderr, got %q", stderr)
	}
}

func TestRunRun_WaitBudgetExhausted(t *testing.T) {
	snapshotRunFlags(t)
	fastPolling(t)
	runWait = 20 * time.Millisecond
	installMockInvokeClient(t, &mockInvokeClient{
		runSyncStep: jobStep{job: mustJob(`{"id":"job-4","status":"IN_PROGRESS"}`)},
		statusSteps: []jobStep{{job: mustJob(`{"id":"job-4","status":"IN_PROGRESS"}`)}},
	})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "timeout" {
		t.Fatalf("code = %q, want timeout (err %v)", code, err)
	}
	// the last known state still goes to stdout so the caller keeps the job id.
	if !strings.Contains(stdout, `"id": "job-4"`) {
		t.Errorf("last known payload missing from stdout: %q", stdout)
	}
}

func TestRunRun_RunSyncRequestTimeoutHasNoJobToPoll(t *testing.T) {
	snapshotRunFlags(t)
	installMockInvokeClient(t, &mockInvokeClient{
		runSyncStep: jobStep{err: api.NewTimeoutError("invoke request timed out: post /ep-1/runsync")},
	})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "timeout" {
		t.Fatalf("code = %q, want timeout (err %v)", code, err)
	}
	if !strings.Contains(err.Error(), "--async") {
		t.Errorf("expected the message to point at --async, got %q", err.Error())
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("nothing is known about the job, so stdout must stay empty: %q", stdout)
	}
}

func TestRunRun_InvalidInputNeverReachesTheAPI(t *testing.T) {
	snapshotRunFlags(t)
	runInput = `{"prompt":`
	client := installMockInvokeClient(t, &mockInvokeClient{})

	var err error
	_, _ = captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "usage_error" {
		t.Fatalf("code = %q, want usage_error (err %v)", code, err)
	}
	if client.runCalls+client.runSyncCalls != 0 {
		t.Errorf("a malformed payload must be caught before any api call, got run=%d runsync=%d", client.runCalls, client.runSyncCalls)
	}
}

func TestRunRun_RejectsNonPositiveWait(t *testing.T) {
	snapshotRunFlags(t)
	runWait = 0

	var err error
	_, _ = captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "usage_error" {
		t.Fatalf("code = %q, want usage_error (err %v)", code, err)
	}
}
