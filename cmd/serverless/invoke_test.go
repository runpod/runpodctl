package serverless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"

	"github.com/spf13/cobra"
)

// jobStep is one scripted answer from the fake invoke api.
type jobStep struct {
	job *api.Job
	err error
}

func mustJob(payload string) *api.Job {
	var job api.Job
	if err := json.Unmarshal([]byte(payload), &job); err != nil {
		panic(err)
	}
	return &job
}

// mockInvokeClient scripts the invoke api so the run/status/health commands can
// be exercised without touching the live service.
type mockInvokeClient struct {
	health      map[string]interface{}
	healthErr   error
	runStep     jobStep
	runSyncStep jobStep
	statusSteps []jobStep

	healthCalls  int
	runCalls     int
	runSyncCalls int
	statusCalls  int
	lastInput    json.RawMessage
	// statusDeadlines records the per-call deadline handed to JobStatus, so a
	// test can assert the last poll of a wait still gets a usable timeout.
	statusDeadlines []time.Duration
}

func (m *mockInvokeClient) EndpointHealth(_ context.Context, _ string) (map[string]interface{}, error) {
	m.healthCalls++
	return m.health, m.healthErr
}

func (m *mockInvokeClient) Run(_ context.Context, _ string, input json.RawMessage) (*api.Job, error) {
	m.runCalls++
	m.lastInput = input
	return m.runStep.job, m.runStep.err
}

func (m *mockInvokeClient) RunSync(_ context.Context, _ string, input json.RawMessage) (*api.Job, error) {
	m.runSyncCalls++
	m.lastInput = input
	return m.runSyncStep.job, m.runSyncStep.err
}

func (m *mockInvokeClient) JobStatus(ctx context.Context, _ string, _ string) (*api.Job, error) {
	if deadline, ok := ctx.Deadline(); ok {
		m.statusDeadlines = append(m.statusDeadlines, time.Until(deadline))
	}
	step := m.statusSteps[len(m.statusSteps)-1]
	if m.statusCalls < len(m.statusSteps) {
		step = m.statusSteps[m.statusCalls]
	}
	m.statusCalls++
	return step.job, step.err
}

// installMockInvokeClient swaps the client factory for the duration of a test.
func installMockInvokeClient(t *testing.T, client *mockInvokeClient) *mockInvokeClient {
	t.Helper()
	old := newInvokeClient
	newInvokeClient = func() (invokeClient, error) { return client, nil }
	t.Cleanup(func() { newInvokeClient = old })
	return client
}

// fastPolling shrinks the poll pacing so a test that needs several polls runs in
// milliseconds instead of seconds.
func fastPolling(t *testing.T) {
	t.Helper()
	oldInitial, oldMax, oldHeartbeat := pollInitialInterval, pollMaxInterval, pollHeartbeat
	t.Cleanup(func() {
		pollInitialInterval, pollMaxInterval, pollHeartbeat = oldInitial, oldMax, oldHeartbeat
	})
	pollInitialInterval, pollMaxInterval, pollHeartbeat = time.Millisecond, 2*time.Millisecond, time.Millisecond
}

// mockInvokeCommand builds a command carrying just what the invoke commands read
// off cobra: the --output flag and stdin.
func mockInvokeCommand(stdin string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")
	cmd.SetIn(strings.NewReader(stdin))
	return cmd
}

// captureOutput runs fn with os.Stdout and os.Stderr redirected, returning both.
// stdout must carry the json payload and nothing else; progress notes and errors
// belong on stderr.
func captureOutput(t *testing.T, fn func()) (stdout string, stderr string) {
	t.Helper()
	outFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		outFile.Close()
		errFile.Close()
	}()

	fn()

	outBytes, err := os.ReadFile(outFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	errBytes, err := os.ReadFile(errFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(outBytes), string(errBytes)
}

// errorCode reads the stable code off an error the way internal/output does.
func errorCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var coder interface{ ErrorCode() string }
	if errors.As(err, &coder) {
		return coder.ErrorCode()
	}
	return "cli_error"
}

func TestWaitForTerminal_PollsUntilTerminal(t *testing.T) {
	fastPolling(t)
	client := &mockInvokeClient{statusSteps: []jobStep{
		{job: mustJob(`{"id":"job-1","status":"IN_QUEUE"}`)},
		{job: mustJob(`{"id":"job-1","status":"IN_PROGRESS"}`)},
		{job: mustJob(`{"id":"job-1","status":"COMPLETED","output":"ok"}`)},
	}}

	job, err := waitForTerminal(client, "ep-1", mustJob(`{"id":"job-1","status":"IN_QUEUE"}`), time.Now().Add(5*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !job.Succeeded() {
		t.Errorf("job status = %q, want COMPLETED", job.Status)
	}
	if client.statusCalls != 3 {
		t.Errorf("status calls = %d, want 3", client.statusCalls)
	}
}

func TestWaitForTerminal_TimeoutIsActionable(t *testing.T) {
	fastPolling(t)
	client := &mockInvokeClient{statusSteps: []jobStep{{job: mustJob(`{"id":"job-1","status":"IN_PROGRESS"}`)}}}

	job, err := waitForTerminal(client, "ep-1", mustJob(`{"id":"job-1","status":"IN_QUEUE"}`), time.Now().Add(20*time.Millisecond))
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if code := errorCode(t, err); code != "timeout" {
		t.Errorf("code = %q, want timeout", code)
	}
	// the message must tell an agent how to keep following the job.
	if !strings.Contains(err.Error(), "runpodctl serverless status ep-1 job-1") {
		t.Errorf("timeout message is not actionable: %q", err.Error())
	}
	if job == nil || job.Status != api.JobStatusInProgress {
		t.Errorf("expected the last seen job to be returned, got %+v", job)
	}
}

func TestWaitForTerminal_RetriesTransientFailures(t *testing.T) {
	fastPolling(t)
	client := &mockInvokeClient{statusSteps: []jobStep{
		{err: &api.APIError{Message: "bad gateway", Status: 502}},
		{err: api.NewTimeoutError("invoke request timed out: get /ep-1/status/job-1")},
		{job: mustJob(`{"id":"job-1","status":"COMPLETED"}`)},
	}}

	job, err := waitForTerminal(client, "ep-1", mustJob(`{"id":"job-1","status":"IN_QUEUE"}`), time.Now().Add(5*time.Second))
	if err != nil {
		t.Fatalf("a transient 502 must not abort the wait: %v", err)
	}
	if !job.Succeeded() {
		t.Errorf("job status = %q, want COMPLETED", job.Status)
	}
	if client.statusCalls != 3 {
		t.Errorf("status calls = %d, want 3", client.statusCalls)
	}
}

func TestWaitForTerminal_FailsFastOnPermanentError(t *testing.T) {
	fastPolling(t)
	client := &mockInvokeClient{statusSteps: []jobStep{
		{err: &api.APIError{Message: "unauthorized", Status: 401}},
	}}

	_, err := waitForTerminal(client, "ep-1", mustJob(`{"id":"job-1","status":"IN_QUEUE"}`), time.Now().Add(5*time.Second))
	if err == nil {
		t.Fatal("expected the 401 to abort the wait")
	}
	if code := errorCode(t, err); code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", code)
	}
	if client.statusCalls != 1 {
		t.Errorf("status calls = %d, want 1 (no retry on 401)", client.statusCalls)
	}
}

func TestWaitForTerminal_NoJobID(t *testing.T) {
	client := &mockInvokeClient{}
	_, err := waitForTerminal(client, "ep-1", mustJob(`{"status":"IN_PROGRESS"}`), time.Now().Add(time.Second))
	if err == nil || !strings.Contains(err.Error(), "without a job id") {
		t.Fatalf("error = %v, want a complaint about the missing job id", err)
	}
	if client.statusCalls != 0 {
		t.Errorf("status calls = %d, want 0", client.statusCalls)
	}
}

func TestRetryablePollError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"cli timeout", api.NewTimeoutError("timed out"), true},
		{"rate limited", &api.APIError{Status: 429}, true},
		{"server error", &api.APIError{Status: 500}, true},
		{"wrapped server error", fmt.Errorf("wrapped: %w", &api.APIError{Status: 503}), true},
		{"not found", &api.APIError{Status: 404}, false},
		{"unauthorized", &api.APIError{Status: 401}, false},
		{"no credentials", api.ErrNoCredentials, false},
		{"connection refused", &url.Error{Op: "Get", Err: &net.OpError{Op: "dial"}}, true},
		{"dns failure", &net.DNSError{Err: "no such host"}, true},
		{"bad url", &url.Error{Op: "parse", Err: errors.New("bad url")}, false},
		{"parse failure", errors.New("failed to parse response"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryablePollError(tt.err); got != tt.want {
				t.Errorf("retryablePollError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestJobOutcome(t *testing.T) {
	tests := []struct {
		status   string
		wantCode string
	}{
		{api.JobStatusCompleted, ""},
		{"", ""},
		// still moving: not a failure, this is what --no-wait returns.
		{api.JobStatusInQueue, ""},
		{api.JobStatusInProgress, ""},
		{api.JobStatusFailed, "job_failed"},
		{api.JobStatusCancelled, "job_failed"},
		{api.JobStatusTimedOut, "job_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			err := jobOutcome(&api.Job{ID: "job-1", Status: tt.status})
			if got := errorCode(t, err); got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestRunHealth(t *testing.T) {
	client := installMockInvokeClient(t, &mockInvokeClient{
		health: map[string]interface{}{
			"jobs":    map[string]interface{}{"inQueue": 0},
			"workers": map[string]interface{}{"idle": 1},
		},
	})

	var err error
	stdout, stderr := captureOutput(t, func() {
		err = runHealth(mockInvokeCommand(""), []string{"ep-1"})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.healthCalls != 1 {
		t.Errorf("health calls = %d, want 1", client.healthCalls)
	}
	if !strings.Contains(stdout, `"idle": 1`) {
		t.Errorf("health payload missing from stdout: %q", stdout)
	}
	if stderr != "" {
		t.Errorf("expected clean stderr, got %q", stderr)
	}
}

func TestRunHealth_ErrorKeepsAPICode(t *testing.T) {
	installMockInvokeClient(t, &mockInvokeClient{healthErr: &api.APIError{Message: "endpoint not found", Status: 404}})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runHealth(mockInvokeCommand(""), []string{"missing"})
	})
	if code := errorCode(t, err); code != "not_found" {
		t.Fatalf("code = %q, want not_found", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("errors must not reach stdout, got %q", stdout)
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		// a sub-second budget must not be reported as "0s".
		{200 * time.Millisecond, "200ms"},
		{1500 * time.Millisecond, "2s"},
		{95 * time.Second, "1m35s"},
	}
	for _, tt := range tests {
		if got := humanDuration(tt.d); got != tt.want {
			t.Errorf("humanDuration(%s) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestWaitForTerminal_BudgetGoneAfterTransientFailure(t *testing.T) {
	fastPolling(t)
	// every poll fails transiently and the budget runs out: the caller still needs
	// the actionable "poll it later" message, not the incidental 502.
	client := &mockInvokeClient{statusSteps: []jobStep{{err: &api.APIError{Message: "bad gateway", Status: 502}}}}

	var err error
	_, stderr := captureOutput(t, func() {
		_, err = waitForTerminal(client, "ep-1", mustJob(`{"id":"job-1","status":"IN_QUEUE"}`), time.Now().Add(5*time.Millisecond))
	})
	if code := errorCode(t, err); code != "timeout" {
		t.Fatalf("code = %q, want timeout (err %v)", code, err)
	}
	if !strings.Contains(err.Error(), "runpodctl serverless status ep-1 job-1") {
		t.Errorf("timeout message is not actionable: %q", err.Error())
	}
	// the swallowed failure must still be visible.
	if !strings.Contains(stderr, "bad gateway") {
		t.Errorf("expected the last failure as a note on stderr, got %q", stderr)
	}
}

func TestPollJobStatus_LastPollKeepsAUsableTimeout(t *testing.T) {
	client := &mockInvokeClient{statusSteps: []jobStep{{job: mustJob(`{"id":"job-1","status":"COMPLETED"}`)}}}

	// a budget of a few milliseconds must not produce a doomed request.
	if _, err := pollJobStatus(client, "ep-1", "job-1", time.Now().Add(2*time.Millisecond)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.statusDeadlines) != 1 {
		t.Fatalf("status deadlines = %v, want one entry", client.statusDeadlines)
	}
	if client.statusDeadlines[0] < minPollRequest/2 {
		t.Errorf("poll deadline = %s, want at least %s", client.statusDeadlines[0], minPollRequest)
	}
}
