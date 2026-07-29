package serverless

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"

	"github.com/spf13/viper"
)

func TestRunCmd_Flags(t *testing.T) {
	flags := runCmd.Flags()

	for _, name := range []string{"input", "input-file", "no-wait", "wait"} {
		if flags.Lookup(name) == nil {
			t.Errorf("expected --%s flag", name)
		}
	}
	if got := flags.Lookup("wait").DefValue; got != api.DefaultInvokeWait.String() {
		t.Errorf("--wait default = %q, want %q", got, api.DefaultInvokeWait.String())
	}
	// /runsync is not used at all, so there is no submit-route switch to offer.
	if flags.Lookup("async") != nil {
		t.Error("--async must not exist: every run submits on /run and polls /status")
	}
}

// snapshotRunFlags restores the run globals after a test and sets a known
// baseline matching the flag defaults.
func snapshotRunFlags(t *testing.T) {
	t.Helper()
	oldInput, oldFile, oldNoWait, oldWait := runInput, runInputFile, runNoWait, runWait
	t.Cleanup(func() {
		runInput, runInputFile, runNoWait, runWait = oldInput, oldFile, oldNoWait, oldWait
	})
	runInput, runInputFile = `{"prompt":"hi"}`, ""
	runNoWait = false
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
		{name: "inline empty object", inline: `{}`, want: `{}`},
		// the api decodes "input" into a json object, so anything else is a 400 —
		// catch it locally instead of paying a round trip to learn that.
		{
			name: "inline array", inline: `[1,2]`,
			wantErr: "must be a json object", wantCode: "usage_error",
		},
		{
			name: "inline string", inline: `"just a string"`,
			wantErr: "must be a json object", wantCode: "usage_error",
		},
		{
			name: "inline number", inline: `42`,
			wantErr: "must be a json object", wantCode: "usage_error",
		},
		// null is not an object either. It happens to be harmless server-side (it
		// decodes to a nil map), but exempting it would make "must be a json object"
		// a rule the cli documents and does not enforce.
		{
			name: "inline null", inline: `null`,
			wantErr: "must be a json object", wantCode: "usage_error",
		},
		{name: "from file", file: payloadPath, want: `{"prompt":"from file"}`},
		{name: "stdin via --input dash", inline: "-", stdin: `{"prompt":"stdin"}`, want: `{"prompt":"stdin"}`},
		{name: "stdin via --input-file dash", file: "-", stdin: `{"prompt":"stdin"}`, want: `{"prompt":"stdin"}`},
		{
			name: "double wrapped payload is flagged", inline: `{"input":{"prompt":"hi"}}`,
			want:     `{"input":{"prompt":"hi"}}`,
			wantNote: `it is sent as {"input": <payload>}`,
		},
		// a whole curl request body is the common paste and has more than one key, so
		// the warning must not be limited to a lone "input".
		{
			name: "curl request envelope is flagged", inline: `{"input":{"prompt":"hi"},"policy":{"ttl":600000}}`,
			want:     `{"input":{"prompt":"hi"},"policy":{"ttl":600000}}`,
			wantNote: `"policy"/"webhook"/"s3Config" keys are ignored inside "input"`,
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
		// a typo'd path is the same class of mistake as a malformed payload: fix the
		// argument and retry. cli_error would tell an agent the environment is broken.
		{
			name: "missing file", file: filepath.Join(dir, "nope.json"),
			wantErr: "failed to read --input-file", wantCode: "usage_error",
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

func TestRunRun_SubmitsOnRunAndWaits(t *testing.T) {
	snapshotRunFlags(t)
	fastPolling(t)
	client := installMockInvokeClient(t, &mockInvokeClient{
		runStep:     jobStep{job: mustJob(`{"id":"job-1","status":"IN_QUEUE"}`)},
		statusSteps: []jobStep{{job: mustJob(`{"id":"job-1","status":"COMPLETED","output":{"echo":"hi"},"executionTime":120}`)}},
	})

	var err error
	stdout, stderr := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.runCalls != 1 {
		t.Errorf("run calls = %d, want 1", client.runCalls)
	}
	// the endpoint id from argv must be the one submitted to and polled.
	assertOnly(t, "run endpoint ids", client.runEndpoints, "ep-1")
	assertOnly(t, "status endpoint ids", client.statusEndpoints, "ep-1")
	assertOnly(t, "status job ids", client.statusJobIDs, "job-1")
	if string(client.lastInput) != `{"prompt":"hi"}` {
		t.Errorf("input sent = %q, want the raw handler payload", client.lastInput)
	}
	if !strings.Contains(stdout, `"echo": "hi"`) || !strings.Contains(stdout, `"executionTime": 120`) {
		t.Errorf("job payload missing from stdout: %q", stdout)
	}
	// progress belongs on stderr only.
	if !strings.Contains(stderr, "waiting for job job-1") {
		t.Errorf("expected progress notes on stderr, got %q", stderr)
	}
	if strings.Contains(stdout, "waiting for job") {
		t.Errorf("progress notes leaked into stdout: %q", stdout)
	}
}

func TestRunRun_PollsUntilTerminal(t *testing.T) {
	snapshotRunFlags(t)
	fastPolling(t)
	client := installMockInvokeClient(t, &mockInvokeClient{
		runStep: jobStep{job: mustJob(`{"id":"job-1","status":"IN_QUEUE"}`)},
		statusSteps: []jobStep{
			{job: mustJob(`{"id":"job-1","status":"IN_PROGRESS"}`)},
			{job: mustJob(`{"id":"job-1","status":"COMPLETED","output":"done"}`)},
		},
	})

	var err error
	stdout, _ := captureOutput(t, func() {
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
}

func TestRunRun_FailedJobPrintsPayloadAndExitsNonZero(t *testing.T) {
	snapshotRunFlags(t)
	fastPolling(t)
	installMockInvokeClient(t, &mockInvokeClient{
		runStep:     jobStep{job: mustJob(`{"id":"job-1","status":"IN_QUEUE"}`)},
		statusSteps: []jobStep{{job: mustJob(`{"id":"job-1","status":"FAILED","error":"handler blew up"}`)}},
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
	if client.runCalls != 1 || client.statusCalls != 0 {
		t.Errorf("calls: run=%d status=%d, want 1/0", client.runCalls, client.statusCalls)
	}
	if !strings.Contains(stdout, `"id": "job-3"`) || !strings.Contains(stdout, `"status": "IN_QUEUE"`) {
		t.Errorf("queued job missing from stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "runpodctl serverless status ep-1 job-3") {
		t.Errorf("expected the follow-up command on stderr, got %q", stderr)
	}
}

func TestRunRun_WaitZeroBehavesLikeNoWait(t *testing.T) {
	// --wait 0 must mean the same thing here as it does on 'serverless status':
	// answer with whatever the api said, do not poll.
	snapshotRunFlags(t)
	runWait = 0
	client := installMockInvokeClient(t, &mockInvokeClient{
		runStep: jobStep{job: mustJob(`{"id":"job-9","status":"IN_QUEUE"}`)},
	})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if err != nil {
		t.Fatalf("--wait 0 must not be an error: %v", err)
	}
	if client.statusCalls != 0 {
		t.Errorf("status calls = %d, want 0 (--wait 0 does not poll)", client.statusCalls)
	}
	if !strings.Contains(stdout, `"id": "job-9"`) {
		t.Errorf("queued job missing from stdout: %q", stdout)
	}
}

func TestRunRun_WaitBudgetExhausted(t *testing.T) {
	snapshotRunFlags(t)
	fastPolling(t)
	runWait = 20 * time.Millisecond
	installMockInvokeClient(t, &mockInvokeClient{
		runStep:     jobStep{job: mustJob(`{"id":"job-4","status":"IN_QUEUE"}`)},
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

func TestRunRun_SubmitTimeoutSaysTheJobIDIsUnknown(t *testing.T) {
	snapshotRunFlags(t)
	installMockInvokeClient(t, &mockInvokeClient{
		runStep: jobStep{err: api.NewTimeoutError("invoke request timed out: post /ep-1/run")},
	})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "timeout" {
		t.Fatalf("code = %q, want timeout (err %v)", code, err)
	}
	// there is no job id, so the message must not promise a pollable job, and must
	// not imply a blind re-invoke is safe.
	if !strings.Contains(err.Error(), "no job id") {
		t.Errorf("expected the message to say there is no job id, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "serverless status") {
		t.Errorf("a submit timeout has nothing to poll, got %q", err.Error())
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("nothing is known about the job, so stdout must stay empty: %q", stdout)
	}
}

func TestRunRun_SubmitIsBoundedByWait(t *testing.T) {
	// the submit must not outlive the advertised --wait bound.
	snapshotRunFlags(t)
	viper.Set("timeout", 30*time.Second)
	t.Cleanup(func() { viper.Set("timeout", nil) })
	runWait = 2 * time.Second
	client := installMockInvokeClient(t, &mockInvokeClient{
		runStep: jobStep{job: mustJob(`{"id":"job-1","status":"COMPLETED"}`)},
	})

	var err error
	_, _ = captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.runDeadlines) != 1 {
		t.Fatalf("run deadlines = %v, want one entry", client.runDeadlines)
	}
	if client.runDeadlines[0] > runWait {
		t.Errorf("submit deadline = %s, want no more than the --wait budget %s", client.runDeadlines[0], runWait)
	}
}

func TestRunRun_NoJobEnvelopeIsAFailure(t *testing.T) {
	// a 200 carrying a bare error object: the body is data and belongs on stdout,
	// but the command must not report success or invent a job id to poll.
	snapshotRunFlags(t)
	client := installMockInvokeClient(t, &mockInvokeClient{
		runStep: jobStep{job: mustJob(`{"error":"endpoint is misconfigured"}`)},
	})

	var err error
	stdout, stderr := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "api_error" {
		t.Fatalf("code = %q, want api_error (err %v)", code, err)
	}
	if !strings.Contains(stdout, "endpoint is misconfigured") {
		t.Errorf("the api body is still the artifact, got stdout %q", stdout)
	}
	if client.statusCalls != 0 {
		t.Errorf("status calls = %d, want 0 (there is no job id to poll)", client.statusCalls)
	}
	if strings.Contains(stderr, "serverless status") {
		t.Errorf("must not print a follow-up command with an empty job id, got %q", stderr)
	}
}

func TestRunRun_NoWaitWithoutJobIDDoesNotAdviseAPoll(t *testing.T) {
	snapshotRunFlags(t)
	runNoWait = true
	installMockInvokeClient(t, &mockInvokeClient{
		runStep: jobStep{job: mustJob(`{"error":"could not queue job"}`)},
	})

	var err error
	_, stderr := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "api_error" {
		t.Fatalf("code = %q, want api_error (err %v)", code, err)
	}
	if strings.Contains(stderr, "serverless status ep-1 ") {
		t.Errorf("advice with an empty job id is unrunnable, got %q", stderr)
	}
}

func TestRunRun_NoWaitWithAStatusButNoJobIDFails(t *testing.T) {
	snapshotRunFlags(t)
	runNoWait = true
	// HasEnvelope is satisfied by a bare status, so this is the one shape jobOutcome
	// cannot catch: a job the api accepted and did not name is submitted, billed and
	// unpollable. Exiting 0 on it is exactly the failure /runsync was dropped over.
	installMockInvokeClient(t, &mockInvokeClient{
		runStep: jobStep{job: mustJob(`{"status":"IN_QUEUE"}`)},
	})

	var err error
	stdout, stderr := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "api_error" {
		t.Fatalf("code = %q, want api_error (err %v)", code, err)
	}
	if !strings.Contains(stdout, "IN_QUEUE") {
		t.Errorf("the payload must still reach stdout, got %q", stdout)
	}
	if strings.Contains(stderr, "serverless status ep-1 ") {
		t.Errorf("advice with an empty job id is unrunnable, got %q", stderr)
	}
}

func TestRunRun_PayloadIsEmittedVerbatim(t *testing.T) {
	// large integers and handler keys that collide with the cli's own control-plane
	// normalisation must survive untouched.
	snapshotRunFlags(t)
	installMockInvokeClient(t, &mockInvokeClient{
		runStep: jobStep{job: mustJob(`{"id":"job-1","status":"COMPLETED","output":{"seed":12345678901234567890,"gpuTypeId":"NVIDIA A40"}}`)},
	})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "12345678901234567890") {
		t.Errorf("a 64-bit integer was reshaped: %q", stdout)
	}
	if !strings.Contains(stdout, `"gpuTypeId"`) || strings.Contains(stdout, `"gpuId"`) {
		t.Errorf("handler keys must not be renamed: %q", stdout)
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
	if client.runCalls != 0 {
		t.Errorf("a malformed payload must be caught before any api call, got run=%d", client.runCalls)
	}
}

func TestRunRun_RejectsNegativeWait(t *testing.T) {
	snapshotRunFlags(t)
	runWait = -time.Second

	var err error
	_, _ = captureOutput(t, func() {
		err = runRun(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "usage_error" {
		t.Fatalf("code = %q, want usage_error (err %v)", code, err)
	}
}
