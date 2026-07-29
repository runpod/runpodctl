package output

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
)

// captureStderr runs fn with os.Stderr replaced by a pipe and returns what it
// wrote. Errors go to stderr so stdout stays a clean data channel.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	fn()

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r) //nolint:errcheck
	return buf.String()
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected Format
	}{
		{"json", FormatJSON},
		{"yaml", FormatYAML},
		{"invalid", FormatJSON}, // defaults to json
		{"", FormatJSON},
	}

	for _, test := range tests {
		result := ParseFormat(test.input)
		if result != test.expected {
			t.Errorf("ParseFormat(%q) = %v, want %v", test.input, result, test.expected)
		}
	}
}

func TestPrint_JSON(t *testing.T) {
	// capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"id": "test-123", "name": "test"}
	err := Print(data, &Config{Format: FormatJSON})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	var result map[string]string
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid json: %v", err)
	}
	if result["id"] != "test-123" {
		t.Errorf("expected test-123, got %s", result["id"])
	}
}

func TestPrint_YAML(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"id": "test-123"}
	err := Print(data, &Config{Format: FormatYAML})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "id: test-123") {
		t.Errorf("yaml output should contain 'id: test-123', got %s", output)
	}
}

func TestPrint_DefaultConfig(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"test": "value"}
	err := Print(data, nil) // nil config should use default (json)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// should be valid json
	var result map[string]string
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("default should be json: %v", err)
	}
}

func TestError(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	Error(fmt.Errorf("test error"))

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, `"error":"test error"`) {
		t.Errorf("expected error json, got %s", output)
	}
}

// codedError implements ErrorCode/HTTPStatus so Error can surface a stable code.
type codedError struct {
	msg    string
	code   string
	status int
}

func (e codedError) Error() string     { return e.msg }
func (e codedError) ErrorCode() string { return e.code }
func (e codedError) HTTPStatus() int   { return e.status }

func TestError_WithCodeAndStatus(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	Error(codedError{msg: "pod not found", code: "not_found", status: 404})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)

	var got map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("error output is not valid json: %v (%s)", err, buf.String())
	}
	if got["error"] != "pod not found" {
		t.Errorf("error = %v, want 'pod not found'", got["error"])
	}
	if got["code"] != "not_found" {
		t.Errorf("code = %v, want 'not_found'", got["code"])
	}
	if status, ok := got["status"].(float64); !ok || int(status) != 404 {
		t.Errorf("status = %v, want 404", got["status"])
	}
}

// A create that succeeded and then failed to become usable leaves a billed
// resource behind. Its id has to be data, not prose an agent has to regex.
func TestError_WithResourceID(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	inner := codedError{msg: "timed out waiting for ssh on pod abc123", code: "wait_timeout"}
	Error(WithResourceID("abc123", fmt.Errorf("%w; pod abc123 was created", inner)))

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r) //nolint:errcheck

	var got map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("error output is not valid json: %v (%s)", err, buf.String())
	}
	if got["id"] != "abc123" {
		t.Errorf("id = %v, want abc123", got["id"])
	}
	// the wrapped error's code must still win: the id is additive.
	if got["code"] != "wait_timeout" {
		t.Errorf("code = %v, want wait_timeout", got["code"])
	}
	if got["error"] != "timed out waiting for ssh on pod abc123; pod abc123 was created" {
		t.Errorf("error = %v, want the wrapped message", got["error"])
	}
}

// No id means no field: every other error keeps the exact shape it had.
func TestWithResourceIDIsANoOpWithoutAnID(t *testing.T) {
	err := errors.New("boom")
	if got := WithResourceID("", err); got != err {
		t.Errorf("WithResourceID with no id must return the error unchanged, got %#v", got)
	}
	if got := WithResourceID("abc123", nil); got != nil {
		t.Errorf("WithResourceID(nil) = %#v, want nil", got)
	}

	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	Error(err)
	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r) //nolint:errcheck
	if strings.Contains(buf.String(), `"id"`) {
		t.Errorf("an unannotated error must carry no id field: %s", buf.String())
	}
}

func TestError_NilIsNoOp(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	Error(nil)

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if buf.Len() != 0 {
		t.Errorf("expected no output for nil error, got %q", buf.String())
	}
}

func TestErrorFallbackCode(t *testing.T) {
	// every emitted error object must carry a code. before this, a hand-written
	// validation error or a transport failure came out as a bare
	// {"error":"..."} and an agent had to read english to tell them apart.
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"plain error", errors.New("invalid --created-after format"), "cli_error"},
		{"wrapped plain error", fmt.Errorf("bad input: %w", errors.New("nope")), "cli_error"},
		{"url error (connection refused)", &url.Error{
			Op:  "Get",
			URL: "http://127.0.0.1:9/v1/endpoints",
			Err: &net.OpError{Op: "dial", Err: errors.New("connect: connection refused")},
		}, "network_error"},
		{"wrapped url error", fmt.Errorf("request failed: %w", &url.Error{
			Op: "Post", URL: "http://x", Err: errors.New("EOF"),
		}), "network_error"},
		{"dns failure", &net.DNSError{Err: "no such host", Name: "nope.invalid"}, "network_error"},
		// a real client timeout arrives wrapped in *url.Error; a BARE
		// context.DeadlineExceeded is a local wait loop giving up and must not be
		// called a network failure (see TestFallbackCode_LocalWaitTimeoutIsNotNetwork).
		{"http client timeout", fmt.Errorf("request failed: %w", &url.Error{Op: "Get", URL: "http://x", Err: context.DeadlineExceeded}), "network_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fallbackCode(tt.err); got != tt.want {
				t.Errorf("fallbackCode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorTypedCodeWinsOverFallback(t *testing.T) {
	// a typed error's own code must never be overwritten by the fallback.
	if got := fallbackCode(errors.New("x")); got != "cli_error" {
		t.Fatalf("precondition: fallbackCode = %q", got)
	}
	stderr := captureStderr(t, func() {
		Error(codedError{msg: "endpoint not found", code: "not_found", status: 404})
	})
	if !strings.Contains(stderr, `"code":"not_found"`) {
		t.Errorf("expected the typed code to survive, got %s", stderr)
	}
	if strings.Contains(stderr, "cli_error") {
		t.Errorf("fallback must not override a typed code, got %s", stderr)
	}
}

func TestFallbackCode_ParseErrorIsNotNetwork(t *testing.T) {
	// url.Parse returns *url.Error with Op "parse". A malformed RUNPOD_API_URL is
	// a local config mistake, and network_error is the one code that tells an
	// agent "transient, retry" — misfiling it here would make an agent retry a
	// permanently broken url forever.
	_, parseErr := url.Parse("http://[::1")
	if parseErr == nil {
		t.Fatal("expected the malformed url to fail parsing")
	}
	wrapped := fmt.Errorf("failed to create request: %w", parseErr)
	if got := fallbackCode(wrapped); got != "cli_error" {
		t.Errorf("fallbackCode(url parse error) = %q, want cli_error", got)
	}

	// a genuine transport failure through the same *url.Error type must still
	// classify as network_error.
	transport := &url.Error{Op: "Get", URL: "http://x", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}
	if got := fallbackCode(fmt.Errorf("request failed: %w", transport)); got != "network_error" {
		t.Errorf("fallbackCode(transport error) = %q, want network_error", got)
	}
}

func TestFallbackCode_LocalWaitTimeoutIsNotNetwork(t *testing.T) {
	// a local wait loop that gives up produces context.DeadlineExceeded without
	// any network involvement — e.g. `model add --wait-for-hash` timing out AFTER
	// a fully successful upload. Classifying that as network_error told the agent
	// "transient, retry", and a retry re-uploads the entire model.
	err := fmt.Errorf("upload completed but timed out waiting for the model hash; the model exists, do not re-upload: %w", context.DeadlineExceeded)
	if got := fallbackCode(err); got != "cli_error" {
		t.Errorf("fallbackCode(local wait timeout) = %q, want cli_error", got)
	}

	// a real http client timeout still classifies as network_error, because
	// net/http wraps it in *url.Error.
	httpTimeout := fmt.Errorf("request failed: %w", &url.Error{
		Op: "Get", URL: "http://x", Err: context.DeadlineExceeded,
	})
	if got := fallbackCode(httpTimeout); got != "network_error" {
		t.Errorf("fallbackCode(http client timeout) = %q, want network_error", got)
	}
}

// captureStdout runs fn with os.Stdout replaced by a pipe and returns what it
// wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestPrintRaw_JSONIsNumberFaithful(t *testing.T) {
	// these are the values a map[string]interface{} round trip destroys: every
	// number becomes a float64, so anything above 2^53 comes out wrong.
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "64-bit seed", raw: `{"seed":12345678901234567890}`, want: "12345678901234567890"},
		{name: "nanosecond timestamp", raw: `{"jobNumber":1753800000000000123}`, want: "1753800000000000123"},
		{name: "just above 2^53", raw: `{"n":9007199254740993}`, want: "9007199254740993"},
		{name: "high precision float", raw: `{"n":0.1000000000000000055511151231257827}`, want: "0.1000000000000000055511151231257827"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				if err := PrintRaw([]byte(tt.raw), &Config{Format: FormatJSON}); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			})
			if !strings.Contains(out, tt.want) {
				t.Errorf("PrintRaw(%s) = %s, want it to contain %s", tt.raw, out, tt.want)
			}
		})
	}
}

func TestPrintRaw_DoesNotRenameGPUKeys(t *testing.T) {
	// Print rewrites gpuTypeId -> gpuId for the cli's own control-plane structs.
	// A serverless handler's output is not ours to rewrite.
	raw := `{"output":{"gpuTypeId":"NVIDIA A40","gpuTypeIds":["NVIDIA A40"]}}`

	out := captureStdout(t, func() {
		if err := PrintRaw([]byte(raw), &Config{Format: FormatJSON}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, `"gpuTypeId"`) || !strings.Contains(out, `"gpuTypeIds"`) {
		t.Errorf("handler keys were renamed: %s", out)
	}
	if strings.Contains(out, `"gpuId"`) || strings.Contains(out, `"gpuIds"`) {
		t.Errorf("gpu-key normalisation must not apply to raw payloads: %s", out)
	}

	// contrast: Print still normalises, which is what the control-plane paths want.
	normalised := captureStdout(t, func() {
		if err := Print(map[string]interface{}{"gpuTypeId": "NVIDIA A40"}, &Config{Format: FormatJSON}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(normalised, `"gpuId"`) {
		t.Errorf("Print should still normalise gpu keys: %s", normalised)
	}
}

func TestPrintRaw_SortsKeys(t *testing.T) {
	out := captureStdout(t, func() {
		if err := PrintRaw([]byte(`{"zeta":1,"alpha":2,"mid":3}`), &Config{Format: FormatJSON}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if got := strings.Index(out, "alpha"); got > strings.Index(out, "mid") {
		t.Errorf("keys are not sorted: %s", out)
	}
}

func TestPrintRaw_YAML(t *testing.T) {
	out := captureStdout(t, func() {
		if err := PrintRaw([]byte(`{"zeta":1,"alpha":{"big":1753800000000000123},"list":[1,"two",true,null]}`), &Config{Format: FormatYAML}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	// numbers stay unquoted literals rather than becoming quoted json.Number
	// strings or rounded floats.
	if !strings.Contains(out, "big: 1753800000000000123") {
		t.Errorf("yaml lost the integer literal: %s", out)
	}
	// deterministic key order: yaml.Marshal over a Go map would be random.
	if strings.Index(out, "alpha:") > strings.Index(out, "zeta:") {
		t.Errorf("yaml keys are not sorted: %s", out)
	}
	if !strings.Contains(out, "- two") {
		t.Errorf("yaml lost the list: %s", out)
	}
}

func TestPrintRaw_NonJSONFallsBackToBytes(t *testing.T) {
	out := captureStdout(t, func() {
		if err := PrintRaw([]byte("<html>gateway timeout</html>"), &Config{Format: FormatJSON}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "<html>gateway timeout</html>") {
		t.Errorf("expected the raw bytes, got %s", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("expected a trailing newline, got %q", out)
	}
}

func TestPrintRaw_NilConfigDefaultsToJSON(t *testing.T) {
	out := captureStdout(t, func() {
		if err := PrintRaw([]byte(`{"a":1}`), nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, `"a": 1`) {
		t.Errorf("expected indented json, got %s", out)
	}
}
