package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/runpod/runpodctl/internal/configenv"
)

// DefaultInvokeWait is how long `serverless run` waits, by default, for a job to
// reach a terminal status. It is deliberately far larger than the control-plane
// DefaultTimeout (30s): a cold endpoint has to pull the image and boot a worker
// before the handler even starts, so 30s would report a failure on a perfectly
// healthy first invocation.
const DefaultInvokeWait = 5 * time.Minute

// runSyncRequestCap bounds a single /runsync http request. The invoke service
// answers /runsync within ~90s and hands back a still-running job instead of
// holding the connection open, so waiting much longer than that on one request
// buys nothing — the remaining wait budget is spent polling /status instead.
const runSyncRequestCap = 100 * time.Second

// job statuses reported by the invoke api.
const (
	JobStatusInQueue    = "IN_QUEUE"
	JobStatusInProgress = "IN_PROGRESS"
	JobStatusCompleted  = "COMPLETED"
	JobStatusFailed     = "FAILED"
	JobStatusCancelled  = "CANCELLED"
	JobStatusTimedOut   = "TIMED_OUT"
)

// InvokeClient calls the serverless invoke service (api.runpod.ai/v2). It is
// separate from Client on purpose: invoke is a different service from the
// control plane (rest.runpod.io/v1), with its own host override
// (RUNPOD_INVOKE_URL) and its own timing profile. Deadlines are per call via
// context instead of one client-wide timeout, because a single /runsync can
// legitimately outlast the control-plane timeout. Auth header, User-Agent and
// error unwrapping are shared with Client so error codes stay uniform.
type InvokeClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	userAgent  string
}

// NewInvokeClient creates a client for the serverless invoke service.
func NewInvokeClient() (*InvokeClient, error) {
	apiKey := configenv.APIKey()
	if apiKey == "" {
		return nil, ErrNoCredentials
	}

	return &InvokeClient{
		baseURL: invokeBaseURL(),
		apiKey:  apiKey,
		// no client-wide timeout: every call carries its own context deadline.
		httpClient: &http.Client{},
		userAgent:  buildUserAgent(),
	}, nil
}

// do issues a request against the invoke service. A context deadline is
// reported as a TimeoutError rather than a bare "context deadline exceeded", so
// an agent can tell "still running, keep waiting" apart from a broken endpoint.
func (c *InvokeClient) do(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, NewTimeoutError("invoke request timed out: %s %s", strings.ToLower(method), path)
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, NewTimeoutError("invoke request timed out: %s %s", strings.ToLower(method), path)
		}
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(respBody, resp.StatusCode)
	}

	return respBody, nil
}

// EndpointHealth returns the health payload for an endpoint (worker and job
// counts). The api body is passed through verbatim so new fields reach the
// caller without a cli release.
func (c *InvokeClient) EndpointHealth(ctx context.Context, endpointID string) (map[string]interface{}, error) {
	data, err := c.do(ctx, http.MethodGet, "/"+endpointID+"/health", nil)
	if err != nil {
		return nil, err
	}

	var health map[string]interface{}
	if err := json.Unmarshal(data, &health); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return health, nil
}

// jobRequest is the invoke wire body. The handler payload is always nested
// under "input".
type jobRequest struct {
	Input json.RawMessage `json:"input"`
}

// Run submits a job asynchronously (POST /run) and returns immediately with the
// queued job.
func (c *InvokeClient) Run(ctx context.Context, endpointID string, input json.RawMessage) (*Job, error) {
	return c.submit(ctx, endpointID, "/run", input)
}

// RunSync submits a job and waits for the result inline (POST /runsync). The
// invoke service gives up holding the connection after ~90s and answers with a
// non-terminal job instead, so callers must be prepared to poll JobStatus. The
// request is capped at runSyncRequestCap on top of the caller's deadline
// (whichever is sooner wins), keeping the knowledge of that server-side limit in
// one place.
func (c *InvokeClient) RunSync(ctx context.Context, endpointID string, input json.RawMessage) (*Job, error) {
	ctx, cancel := context.WithTimeout(ctx, runSyncRequestCap)
	defer cancel()
	return c.submit(ctx, endpointID, "/runsync", input)
}

func (c *InvokeClient) submit(ctx context.Context, endpointID, route string, input json.RawMessage) (*Job, error) {
	data, err := c.do(ctx, http.MethodPost, "/"+endpointID+route, jobRequest{Input: input})
	if err != nil {
		return nil, err
	}
	return parseJob(data)
}

// JobStatus fetches the current state of a previously submitted job
// (GET /status/<job-id>).
func (c *InvokeClient) JobStatus(ctx context.Context, endpointID, jobID string) (*Job, error) {
	data, err := c.do(ctx, http.MethodGet, "/"+endpointID+"/status/"+jobID, nil)
	if err != nil {
		return nil, err
	}
	return parseJob(data)
}

func parseJob(data []byte) (*Job, error) {
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &job, nil
}

// Job is a serverless job as reported by the invoke api. ID and Status are the
// only fields the cli branches on; everything else (notably `output`, whose
// shape is entirely up to the handler) is kept in Payload and emitted verbatim,
// so a typed struct here can never silently drop a field an agent needs.
type Job struct {
	ID      string
	Status  string
	Payload map[string]interface{}
}

// UnmarshalJSON keeps the whole api body in Payload while lifting the two
// fields the cli needs.
func (j *Job) UnmarshalJSON(data []byte) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	j.Payload = payload
	j.ID, _ = payload["id"].(string)
	j.Status, _ = payload["status"].(string)
	return nil
}

// MarshalJSON emits the api body unchanged.
func (j Job) MarshalJSON() ([]byte, error) {
	if j.Payload == nil {
		return []byte("null"), nil
	}
	return json.Marshal(j.Payload)
}

// IsTerminal reports whether the job has stopped moving and polling it again is
// pointless. An empty status counts as terminal: a 200 with no status envelope
// has no job to poll (some handlers and proxied routes answer with the raw
// output), and treating it as pending would spin until the wait budget expired.
// An *unrecognised* non-empty status is deliberately treated as non-terminal, so
// a new queue state added server-side is polled rather than mistaken for a
// result; the wait budget bounds that.
func (j *Job) IsTerminal() bool {
	switch strings.ToUpper(j.Status) {
	case "", JobStatusCompleted, JobStatusFailed, JobStatusCancelled, JobStatusTimedOut:
		return true
	}
	return false
}

// Succeeded reports whether the job completed successfully.
func (j *Job) Succeeded() bool {
	return strings.ToUpper(j.Status) == JobStatusCompleted
}

// TimeoutError is returned when the cli gave up waiting rather than the request
// failing: either a single invoke request exceeded its deadline, or the wait
// budget for a job elapsed while the job was still running. It carries the
// "timeout" code so an agent can tell that apart from a broken endpoint — the
// work may well still be running server-side, and the right move is to poll it,
// not to redeploy. See the error-code vocabulary in client.go.
type TimeoutError struct {
	Message string
}

// Error implements the error interface.
func (e *TimeoutError) Error() string { return e.Message }

// ErrorCode returns the stable code for a cli-side timeout.
func (e *TimeoutError) ErrorCode() string { return "timeout" }

// NewTimeoutError builds a TimeoutError with a formatted message.
func NewTimeoutError(format string, args ...interface{}) *TimeoutError {
	return &TimeoutError{Message: fmt.Sprintf(format, args...)}
}

// JobFailedError is returned when a job reached a terminal status other than
// COMPLETED. The request itself succeeded, so this is not an APIError: the job
// payload (including whatever the worker put in `error`) is still printed to
// stdout, and this only carries the non-zero exit and the "job_failed" code.
// See the error-code vocabulary in client.go.
type JobFailedError struct {
	JobID  string
	Status string
}

// Error implements the error interface.
func (e *JobFailedError) Error() string {
	if e.JobID == "" {
		return fmt.Sprintf("job finished with status %s", e.Status)
	}
	return fmt.Sprintf("job %s finished with status %s", e.JobID, e.Status)
}

// ErrorCode returns the stable code for a job that did not complete.
func (e *JobFailedError) ErrorCode() string { return "job_failed" }

// NewJobFailedError builds a JobFailedError for a terminal, non-completed job.
func NewJobFailedError(jobID, status string) *JobFailedError {
	return &JobFailedError{JobID: jobID, Status: status}
}
