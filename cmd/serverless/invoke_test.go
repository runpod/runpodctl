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
	"github.com/spf13/viper"
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
//
// It records every identifier it is handed and honours the context deadline. That
// matters: a mock that ignores its arguments cannot tell a working command from
// one that passes the endpoint id where the job id belongs, or that hands the api
// an already-expired deadline — both of which are invisible in coverage and fatal
// in production.
type mockInvokeClient struct {
	health      json.RawMessage
	healthErr   error
	runStep     jobStep
	statusSteps []jobStep

	healthCalls int
	runCalls    int
	statusCalls int
	lastInput   json.RawMessage
	// every endpoint id / job id the command asked for, in call order.
	healthEndpoints []string
	runEndpoints    []string
	statusEndpoints []string
	statusJobIDs    []string
	// statusDeadlines records the per-call deadline handed to JobStatus, so a
	// test can assert the last poll of a wait still gets a usable timeout.
	statusDeadlines []time.Duration
	runDeadlines    []time.Duration
}

// contextErr fails a call whose deadline is already spent, the way a real http
// client would.
func contextErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return api.NewTimeoutError("invoke request timed out: %v", err)
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= 0 {
		return api.NewTimeoutError("invoke request timed out: deadline already passed")
	}
	return nil
}

func (m *mockInvokeClient) EndpointHealth(ctx context.Context, endpointID string) (json.RawMessage, error) {
	m.healthCalls++
	m.healthEndpoints = append(m.healthEndpoints, endpointID)
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	return m.health, m.healthErr
}

func (m *mockInvokeClient) Run(ctx context.Context, endpointID string, input json.RawMessage) (*api.Job, error) {
	m.runCalls++
	m.runEndpoints = append(m.runEndpoints, endpointID)
	m.lastInput = input
	if deadline, ok := ctx.Deadline(); ok {
		m.runDeadlines = append(m.runDeadlines, time.Until(deadline))
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	return m.runStep.job, m.runStep.err
}

func (m *mockInvokeClient) JobStatus(ctx context.Context, endpointID, jobID string) (*api.Job, error) {
	m.statusEndpoints = append(m.statusEndpoints, endpointID)
	m.statusJobIDs = append(m.statusJobIDs, jobID)
	if deadline, ok := ctx.Deadline(); ok {
		m.statusDeadlines = append(m.statusDeadlines, time.Until(deadline))
	}
	if err := contextErr(ctx); err != nil {
		m.statusCalls++
		return nil, err
	}
	step := m.statusSteps[len(m.statusSteps)-1]
	if m.statusCalls < len(m.statusSteps) {
		step = m.statusSteps[m.statusCalls]
	}
	m.statusCalls++
	return step.job, step.err
}

// assertOnly checks that a recorded argument list is exactly the same value
// repeated, which is what every command in this package should produce.
func assertOnly(t *testing.T, what string, got []string, want string) {
	t.Helper()
	if len(got) == 0 {
		t.Errorf("%s: no calls recorded, want %q", what, want)
		return
	}
	for _, value := range got {
		if value != want {
			t.Errorf("%s = %q, want every call to use %q", what, got, want)
			return
		}
	}
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
		health: json.RawMessage(`{"jobs":{"inQueue":0},"workers":{"idle":1}}`),
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
	// the id the caller passed is the id that must be queried.
	assertOnly(t, "health endpoint ids", client.healthEndpoints, "ep-1")
	if !strings.Contains(stdout, `"idle": 1`) {
		t.Errorf("health payload missing from stdout: %q", stdout)
	}
	if stderr != "" {
		t.Errorf("expected clean stderr, got %q", stderr)
	}
}

func TestRunHealth_RespectsRequestTimeout(t *testing.T) {
	// a sub-millisecond per-call timeout must actually reach the api call, so a
	// gutted requestTimeout() cannot pass unnoticed.
	viper.Set("timeout", time.Nanosecond)
	t.Cleanup(func() { viper.Set("timeout", nil) })
	installMockInvokeClient(t, &mockInvokeClient{health: json.RawMessage(`{}`)})

	var err error
	_, _ = captureOutput(t, func() {
		err = runHealth(mockInvokeCommand(""), []string{"ep-1"})
	})
	if code := errorCode(t, err); code != "timeout" {
		t.Fatalf("code = %q, want timeout (err %v)", code, err)
	}
}

func TestRunHealth_NonJSONBodyIsNotSwallowed(t *testing.T) {
	// PrintRaw falls back to the bytes as-is rather than failing the command.
	installMockInvokeClient(t, &mockInvokeClient{health: json.RawMessage(`<html>oops`)})

	var err error
	stdout, _ := captureOutput(t, func() {
		err = runHealth(mockInvokeCommand(""), []string{"ep-1"})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "<html>oops") {
		t.Errorf("expected the raw body on stdout, got %q", stdout)
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
	if client.statusDeadlines[0] < minRequest/2 {
		t.Errorf("poll deadline = %s, want at least %s", client.statusDeadlines[0], minRequest)
	}
}

func TestBoundedRequestTimeout(t *testing.T) {
	viper.Set("timeout", 30*time.Second)
	t.Cleanup(func() { viper.Set("timeout", nil) })

	tests := []struct {
		name    string
		budget  time.Duration
		wantMax time.Duration
	}{
		// a --wait smaller than the per-call timeout must shrink the call, or the
		// command runs long past the bound it advertises.
		{name: "budget below per-call timeout", budget: 2 * time.Second, wantMax: 2 * time.Second},
		{name: "budget above per-call timeout", budget: 10 * time.Minute, wantMax: 30 * time.Second},
		// but never below the floor, or the request is doomed before it is sent.
		{name: "budget already gone", budget: 0, wantMax: minRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := boundedRequestTimeout(time.Now().Add(tt.budget))
			if got > tt.wantMax {
				t.Errorf("timeout = %s, want at most %s", got, tt.wantMax)
			}
			if got < minRequest {
				t.Errorf("timeout = %s, want at least the %s floor", got, minRequest)
			}
		})
	}
}

func TestFetchJobStatus_WithoutBudgetUsesTheFullPerCallTimeout(t *testing.T) {
	// a command with no wait budget (a plain status check, --no-wait) must still get
	// the ordinary per-call timeout. Clamping it to whatever is left of a budget
	// that never existed would give one legitimate api call the minRequest floor.
	viper.Set("timeout", 30*time.Second)
	t.Cleanup(func() { viper.Set("timeout", nil) })
	client := &mockInvokeClient{statusSteps: []jobStep{{job: mustJob(`{"id":"job-1","status":"IN_QUEUE"}`)}}}

	if _, err := fetchJobStatus(client, "ep-1", "job-1", time.Now(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.statusDeadlines) != 1 {
		t.Fatalf("status deadlines = %v, want one entry", client.statusDeadlines)
	}
	if client.statusDeadlines[0] <= minRequest {
		t.Errorf("request deadline = %s, want the full per-call timeout, not the %s floor",
			client.statusDeadlines[0], minRequest)
	}
}

func TestFetchJobStatus_RetriesTransientFailureWhileBudgetRemains(t *testing.T) {
	fastPolling(t)
	client := &mockInvokeClient{statusSteps: []jobStep{
		{err: &api.APIError{Message: "bad gateway", Status: 502}},
		{job: mustJob(`{"id":"job-1","status":"COMPLETED"}`)},
	}}

	var (
		job *api.Job
		err error
	)
	_, stderr := captureOutput(t, func() {
		job, err = fetchJobStatus(client, "ep-1", "job-1", time.Now().Add(5*time.Second), true)
	})
	if err != nil {
		t.Fatalf("a transient 502 must not abort the first check: %v", err)
	}
	if !job.Succeeded() {
		t.Errorf("job status = %q, want COMPLETED", job.Status)
	}
	if !strings.Contains(stderr, "retrying") {
		t.Errorf("expected a retry note on stderr, got %q", stderr)
	}
}

func TestFetchJobStatus_NoBudgetFailsFast(t *testing.T) {
	client := &mockInvokeClient{statusSteps: []jobStep{
		{err: &api.APIError{Message: "bad gateway", Status: 502}},
		{job: mustJob(`{"id":"job-1","status":"COMPLETED"}`)},
	}}

	// without --wait there is no budget to retry inside: one call, report it.
	_, err := fetchJobStatus(client, "ep-1", "job-1", time.Now(), false)
	if code := errorCode(t, err); code != "server_error" {
		t.Fatalf("code = %q, want server_error (err %v)", code, err)
	}
	if client.statusCalls != 1 {
		t.Errorf("status calls = %d, want 1 (no retry without a budget)", client.statusCalls)
	}
}

func TestFetchJobStatus_PermanentErrorFailsFastWithBudget(t *testing.T) {
	fastPolling(t)
	client := &mockInvokeClient{statusSteps: []jobStep{{err: &api.APIError{Message: "job not found", Status: 404}}}}

	_, err := fetchJobStatus(client, "ep-1", "job-1", time.Now().Add(5*time.Second), true)
	if code := errorCode(t, err); code != "not_found" {
		t.Fatalf("code = %q, want not_found (err %v)", code, err)
	}
	if client.statusCalls != 1 {
		t.Errorf("status calls = %d, want 1 (a 404 will never fix itself)", client.statusCalls)
	}
}

func TestJobOutcome_NoEnvelopeIsAFailure(t *testing.T) {
	// a 200 carrying a bare error object is not a finished job.
	err := jobOutcome(mustJob(`{"error":"endpoint is misconfigured"}`))
	if code := errorCode(t, err); code != "api_error" {
		t.Fatalf("code = %q, want api_error (err %v)", code, err)
	}
}
