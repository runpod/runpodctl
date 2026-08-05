package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/configenv"
)

// newTestInvokeClient points an invoke client at a test server, exercising the
// same base-url resolution the real client uses (RUNPOD_INVOKE_URL), not a
// hand-set field.
func newTestInvokeClient(t *testing.T, baseURL string) *InvokeClient {
	t.Helper()
	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv(configenv.InvokeURLEnv, baseURL)

	client, err := NewInvokeClient()
	if err != nil {
		t.Fatalf("failed to create invoke client: %v", err)
	}
	return client
}

func TestNewInvokeClient_RequiresAPIKey(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "")

	_, err := NewInvokeClient()
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}
}

func TestNewInvokeClient_DefaultsToProdInvokeHost(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv(configenv.InvokeURLEnv, "")

	client, err := NewInvokeClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.baseURL != ServerlessInvokeBaseURL {
		t.Errorf("baseURL = %q, want %q", client.baseURL, ServerlessInvokeBaseURL)
	}
}

func TestInvokeClient_EndpointHealth(t *testing.T) {
	var gotAuth, gotUserAgent, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUserAgent = r.Header.Get("User-Agent")
		gotPath = r.URL.Path
		w.Write([]byte(`{"jobs":{"completed":2,"inQueue":0},"workers":{"idle":1,"ready":1}}`))
	}))
	defer server.Close()

	client := newTestInvokeClient(t, server.URL)

	health, err := client.EndpointHealth(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/ep-1/health" {
		t.Errorf("path = %q, want /ep-1/health", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if !strings.HasPrefix(gotUserAgent, "runpod-cli/") {
		t.Errorf("user-agent = %q, want the shared runpod-cli agent", gotUserAgent)
	}
	// the body is handed back as the exact bytes the api sent.
	if string(health) != `{"jobs":{"completed":2,"inQueue":0},"workers":{"idle":1,"ready":1}}` {
		t.Errorf("health body not passed through verbatim: %s", health)
	}
}

func TestInvokeClient_EndpointHealthRejectsNonJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>gateway</html>`))
	}))
	defer server.Close()

	client := newTestInvokeClient(t, server.URL)

	if _, err := client.EndpointHealth(context.Background(), "ep-1"); err == nil {
		t.Fatal("expected a parse error for a non-json health body")
	}
}

func TestInvokeClient_ErrorsUseSharedCodes(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantCode   string
		wantStatus int
		wantMsg    string
	}{
		{"not found", http.StatusNotFound, `{"error":"endpoint not found"}`, "not_found", 404, "endpoint not found"},
		{"unauthorized", http.StatusUnauthorized, `{"error":"unauthorized"}`, "unauthorized", 401, "unauthorized"},
		{"server error", http.StatusBadGateway, ``, "server_error", 502, "api request failed with status 502"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := newTestInvokeClient(t, server.URL)

			_, err := client.EndpointHealth(context.Background(), "ep-1")
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *APIError, got %v", err)
			}
			if apiErr.ErrorCode() != tt.wantCode {
				t.Errorf("code = %q, want %q", apiErr.ErrorCode(), tt.wantCode)
			}
			if apiErr.HTTPStatus() != tt.wantStatus {
				t.Errorf("status = %d, want %d", apiErr.HTTPStatus(), tt.wantStatus)
			}
			if apiErr.Error() != tt.wantMsg {
				t.Errorf("message = %q, want %q", apiErr.Error(), tt.wantMsg)
			}
		})
	}
}

func TestInvokeClient_RunSubmitsOnRunWithNestedInput(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Write([]byte(`{"id":"job-1","status":"IN_QUEUE"}`))
	}))
	defer server.Close()

	job, err := newTestInvokeClient(t, server.URL).Run(context.Background(), "ep-1", json.RawMessage(`{"prompt":"hi"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// /runsync is deliberately never called: a request that times out on it leaves
	// a billed job with no id to poll, and its result expires after 1 minute.
	if gotPath != "/ep-1/run" {
		t.Errorf("path = %q, want /ep-1/run", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	// the handler payload must be nested under "input" exactly once.
	if gotBody != `{"input":{"prompt":"hi"}}` {
		t.Errorf("body = %q, want {\"input\":{\"prompt\":\"hi\"}}", gotBody)
	}
	if job.ID != "job-1" || job.Status != JobStatusInQueue {
		t.Errorf("job = %+v, want id job-1 status IN_QUEUE", job)
	}
}

func TestInvokeClient_JobStatusPreservesPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ep-1/status/job-1" {
			t.Errorf("path = %q, want /ep-1/status/job-1", r.URL.Path)
		}
		w.Write([]byte(`{"id":"job-1","status":"COMPLETED","output":{"echo":"hi"},"executionTime":42,"someNewField":true}`))
	}))
	defer server.Close()

	client := newTestInvokeClient(t, server.URL)

	job, err := client.JobStatus(context.Background(), "ep-1", "job-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !job.Succeeded() {
		t.Errorf("expected a completed job, got status %q", job.Status)
	}

	// Raw() must reproduce the api body, including fields the cli has no
	// struct field for — agents consume `output` and whatever else the api adds.
	encoded := job.Raw()
	var decoded map[string]interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("failed to decode marshalled job: %v", err)
	}
	if decoded["someNewField"] != true {
		t.Errorf("unknown field dropped: %s", encoded)
	}
	out, ok := decoded["output"].(map[string]interface{})
	if !ok || out["echo"] != "hi" {
		t.Errorf("handler output dropped: %s", encoded)
	}
	if decoded["executionTime"] != float64(42) {
		t.Errorf("executionTime dropped: %s", encoded)
	}
}

func TestJobRawIsByteFaithful(t *testing.T) {
	// Raw() must reproduce the api body exactly — it is what both print sites
	// hand to PrintRaw. Decoding a handler payload into map[string]interface{}
	// and re-encoding it would turn these integers into float64 and quietly
	// corrupt them.
	bodies := []string{
		`{"id":"job-1","status":"COMPLETED","output":{"seed":12345678901234567890}}`,
		`{"id":"job-1","status":"COMPLETED","output":{"jobNumber":1753800000000000123}}`,
		`{"id":"job-1","status":"COMPLETED","output":{"nested":[{"big":9007199254740993}]}}`,
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			var job Job
			if err := json.Unmarshal([]byte(body), &job); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(job.Raw()) != body {
				t.Errorf("Raw() = %s, want the api body verbatim %s", job.Raw(), body)
			}
		})
	}
}

func TestJobHasEnvelope(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "id and status", body: `{"id":"job-1","status":"IN_QUEUE"}`, want: true},
		{name: "status only", body: `{"status":"IN_QUEUE"}`, want: true},
		{name: "id only", body: `{"id":"job-1"}`, want: true},
		// a 200 carrying a bare error object is not a job.
		{name: "error body", body: `{"error":"could not queue job"}`, want: false},
		{name: "empty object", body: `{}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var job Job
			if err := json.Unmarshal([]byte(tt.body), &job); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := job.HasEnvelope(); got != tt.want {
				t.Errorf("HasEnvelope() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobUnmarshalToleratesNonStringIDs(t *testing.T) {
	// a surprising type must not fail the whole decode: the payload is still the
	// artifact the caller wants to see.
	var job Job
	if err := json.Unmarshal([]byte(`{"id":42,"status":null,"output":"ok"}`), &job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.ID != "" || job.Status != "" {
		t.Errorf("job = %+v, want empty lifted fields", job)
	}
	if !job.HasEnvelope() {
		t.Error("a body with id/status keys still has an envelope")
	}
}

func TestNoJobEnvelopeErrorUsesAPIErrorCode(t *testing.T) {
	err := NewNoJobEnvelopeError()
	if err.ErrorCode() != "api_error" {
		t.Errorf("code = %q, want api_error", err.ErrorCode())
	}
}

func TestInvokeClient_TimeoutIsCoded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newTestInvokeClient(t, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := client.EndpointHealth(ctx, "ep-1")
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected *TimeoutError, got %v", err)
	}
	if timeoutErr.ErrorCode() != "timeout" {
		t.Errorf("code = %q, want timeout", timeoutErr.ErrorCode())
	}
	// a bare "context deadline exceeded" is exactly what this must not surface.
	if strings.Contains(timeoutErr.Error(), "context deadline exceeded") {
		t.Errorf("message leaks the go error: %q", timeoutErr.Error())
	}
}

func TestJobIsTerminal(t *testing.T) {
	tests := []struct {
		status       string
		wantTerminal bool
		wantSuccess  bool
	}{
		{JobStatusInQueue, false, false},
		{JobStatusInProgress, false, false},
		{JobStatusCompleted, true, true},
		{JobStatusFailed, true, false},
		{JobStatusCancelled, true, false},
		{JobStatusTimedOut, true, false},
		{"completed", true, true},
		// no status envelope at all: nothing to poll, treated as terminal.
		{"", true, false},
		// an unrecognised status must stay pollable rather than be mistaken for a
		// result.
		{"SOME_NEW_QUEUE_STATE", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			job := &Job{Status: tt.status}
			if job.IsTerminal() != tt.wantTerminal {
				t.Errorf("IsTerminal() = %v, want %v", job.IsTerminal(), tt.wantTerminal)
			}
			if job.Succeeded() != tt.wantSuccess {
				t.Errorf("Succeeded() = %v, want %v", job.Succeeded(), tt.wantSuccess)
			}
		})
	}
}

func TestJobFailedError(t *testing.T) {
	err := NewJobFailedError("job-1", JobStatusFailed)
	if err.ErrorCode() != "job_failed" {
		t.Errorf("code = %q, want job_failed", err.ErrorCode())
	}
	if err.Error() != "job job-1 finished with status FAILED" {
		t.Errorf("message = %q", err.Error())
	}
	if got := NewJobFailedError("", JobStatusTimedOut).Error(); got != "job finished with status TIMED_OUT" {
		t.Errorf("message without id = %q", got)
	}
}

// An id is interpolated into the url path, so anything path-significant in it is
// escaped rather than sent raw. (The invoke service routes on the decoded path, so
// this is about what goes on the wire and through proxies, not a guarantee about
// what the server does with it.)
func TestInvokeClient_EscapesIDsInPaths(t *testing.T) {
	cases := []struct {
		name     string
		call     func(*InvokeClient) error
		wantPath string
	}{
		{
			name: "status escapes both ids",
			call: func(c *InvokeClient) error {
				_, err := c.JobStatus(context.Background(), "ep-1/../v1", "job-1/status")
				return err
			},
			wantPath: "/ep-1%2F..%2Fv1/status/job-1%2Fstatus",
		},
		{
			name: "run escapes the endpoint id",
			call: func(c *InvokeClient) error {
				_, err := c.Run(context.Background(), "ep-1/../v1", json.RawMessage(`{}`))
				return err
			},
			wantPath: "/ep-1%2F..%2Fv1/run",
		},
		{
			name: "health escapes the endpoint id",
			call: func(c *InvokeClient) error {
				_, err := c.EndpointHealth(context.Background(), "ep-1/../v1")
				return err
			},
			wantPath: "/ep-1%2F..%2Fv1/health",
		},
		{
			// the ids users actually pass must be untouched, or every existing
			// caller's url changes.
			name: "an ordinary id is sent verbatim",
			call: func(c *InvokeClient) error {
				_, err := c.JobStatus(context.Background(), "ep-1", "sync-8a7f6c21-b3d4-4e11-9f02-1a2b3c4d5e6f-e1")
				return err
			},
			wantPath: "/ep-1/status/sync-8a7f6c21-b3d4-4e11-9f02-1a2b3c4d5e6f-e1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPath := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// EscapedPath, not Path: Path is the decoded form, which cannot show
				// whether the escaping happened.
				gotPath = r.URL.EscapedPath()
				_, _ = w.Write([]byte(`{"id":"job-1","status":"COMPLETED"}`))
			}))
			defer server.Close()

			client := newTestInvokeClient(t, server.URL)
			if err := tc.call(client); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}

// The /run body limit is the invoke service's, so the constant a caller checks
// against has to match it.
func TestMaxRunBodyBytesMatchesTheAPI(t *testing.T) {
	if MaxRunBodyBytes != 10<<20 {
		t.Errorf("MaxRunBodyBytes = %d, want 10 MiB (ai-api LimitBodySize(10*MiB) on POST /run)", MaxRunBodyBytes)
	}
}

// RunBodySize has to report what Run really sends, not an estimate: json
// compaction shrinks a pretty-printed payload and html escaping grows an
// ampersand-heavy one, so len(payload)+envelope is wrong in both directions.
func TestRunBodySizeMatchesWhatRunSends(t *testing.T) {
	cases := []string{
		`{}`,
		`{"prompt":"hi"}`,
		"{\n  \"prompt\": \"pretty printed\",\n  \"n\": 1\n}",
		`{"html":"<b>&amp;</b>"}`,
		`{"unicode":"\u2028\u2029"}`,
	}

	for _, payload := range cases {
		var gotBody []byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(`{"id":"job-1","status":"IN_QUEUE"}`))
		}))

		client := newTestInvokeClient(t, server.URL)
		if _, err := client.Run(context.Background(), "ep-1", json.RawMessage(payload)); err != nil {
			server.Close()
			t.Fatalf("payload %s: unexpected error: %v", payload, err)
		}
		server.Close()

		size, err := RunBodySize(json.RawMessage(payload))
		if err != nil {
			t.Fatalf("payload %s: %v", payload, err)
		}
		if size != len(gotBody) {
			t.Errorf("payload %s: RunBodySize = %d, but the request body was %d bytes (%q)", payload, size, len(gotBody), gotBody)
		}
	}
}
