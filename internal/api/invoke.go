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
// healthy first invocation. Measured against a cold cpu worker on the public
// mock image, the cold start alone was ~95s.
const DefaultInvokeWait = 5 * time.Minute

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
// counts). The api body is returned as raw json so new fields reach the caller
// without a cli release and no value is reshaped on the way through; it is only
// validated as parseable json.
func (c *InvokeClient) EndpointHealth(ctx context.Context, endpointID string) (json.RawMessage, error) {
	data, err := c.do(ctx, http.MethodGet, "/"+endpointID+"/health", nil)
	if err != nil {
		return nil, err
	}

	if !json.Valid(data) {
		return nil, fmt.Errorf("failed to parse response: invoke api returned a non-json health body")
	}

	return json.RawMessage(data), nil
}

// jobRequest is the invoke wire body. The handler payload is always nested
// under "input".
type jobRequest struct {
	Input json.RawMessage `json:"input"`
}

// Run submits a job (POST /run) and returns immediately with the queued job,
// whose id is what JobStatus needs.
//
// The cli deliberately never uses /runsync, even for "wait for the result".
// /runsync is not synchronous: the invoke service holds the connection for 90s
// and then answers with a still-running job, so a caller has to poll /status
// anyway. Worse, until that response arrives there is no job id, so a request
// that times out leaves a submitted, billed job that cannot be polled at all;
// and a job submitted on /runsync gets a "sync-" id whose result is discarded 1
// minute after it completes, against 30 minutes for /run. Submitting here costs
// one extra round trip and makes both of those failure modes impossible.
func (c *InvokeClient) Run(ctx context.Context, endpointID string, input json.RawMessage) (*Job, error) {
	data, err := c.do(ctx, http.MethodPost, "/"+endpointID+"/run", jobRequest{Input: input})
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
// only fields the cli branches on; the rest of the body — notably `output`,
// whose shape is entirely up to the handler — is kept as the raw bytes the api
// sent and emitted from there.
//
// The raw bytes matter: decoding an arbitrary handler payload into
// map[string]interface{} and re-encoding it turns every number into a float64,
// which silently corrupts any integer above 2^53 (an int64 id, a nanosecond
// timestamp, a 64-bit seed). Keeping the body verbatim is the only way the
// "emitted unchanged" promise actually holds.
type Job struct {
	ID     string
	Status string

	raw    json.RawMessage
	fields map[string]json.RawMessage
}

// UnmarshalJSON keeps the api body byte-for-byte while lifting the two fields
// the cli needs. A field that is not a string where the cli expects one is left
// empty rather than failing the whole decode.
func (j *Job) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	j.raw = append(json.RawMessage(nil), data...)
	j.fields = fields
	_ = json.Unmarshal(fields["id"], &j.ID)
	_ = json.Unmarshal(fields["status"], &j.Status)
	return nil
}

// MarshalJSON emits the api body unchanged, byte for byte.
func (j Job) MarshalJSON() ([]byte, error) {
	if len(j.raw) == 0 {
		return []byte("null"), nil
	}
	return j.raw, nil
}

// Raw returns the api body as received.
func (j *Job) Raw() json.RawMessage { return j.raw }

// Field returns a top-level field of the api body as raw json.
func (j *Job) Field(name string) (json.RawMessage, bool) {
	value, ok := j.fields[name]
	return value, ok
}

// HasEnvelope reports whether the body actually describes a job, i.e. carries an
// id or a status. The invoke api can answer 200 with a bare error object (no id,
// no status); that is not a job, and treating it as a finished one would exit 0
// on a failed request and hand back a job id that does not exist.
func (j *Job) HasEnvelope() bool {
	// a lifted value is enough on its own, which also keeps a Job built in code
	// (rather than decoded from a response) from looking like an empty envelope.
	if j.ID != "" || j.Status != "" {
		return true
	}
	if _, ok := j.fields["id"]; ok {
		return true
	}
	_, ok := j.fields["status"]
	return ok
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

// NewNoJobEnvelopeError reports a 200 response that does not describe a job (no
// id and no status). It reuses the existing api_error code — the request nominally
// succeeded but the api did not answer with a job, which is a failure of the api,
// not of the cli or the caller. The body itself is still printed to stdout.
func NewNoJobEnvelopeError() *APIError {
	return &APIError{
		Message: "invoke api returned a 200 response with no job id or status, so there is no job to report or poll",
		Code:    "api_error",
		Status:  http.StatusOK,
	}
}

// NewNoJobIDError reports a submit that was accepted but came back without a job
// id. HasEnvelope is satisfied by a bare status, so this case would otherwise look
// like a clean submission — while the job it created can never be polled. Same
// api_error code and reasoning as NewNoJobEnvelopeError.
func NewNoJobIDError() *APIError {
	return &APIError{
		Message: "invoke api accepted the job but returned no job id, so it cannot be polled or followed up",
		Code:    "api_error",
		Status:  http.StatusOK,
	}
}
