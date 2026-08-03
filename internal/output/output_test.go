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
		// case and whitespace are normalized: these used to fall through to json
		{"YAML", FormatYAML},
		{"Yaml", FormatYAML},
		{" yaml ", FormatYAML},
		{"JSON", FormatJSON},
	}

	for _, test := range tests {
		result := ParseFormat(test.input)
		if result != test.expected {
			t.Errorf("ParseFormat(%q) = %v, want %v", test.input, result, test.expected)
		}
	}
}

func TestValidateFormat(t *testing.T) {
	valid := []string{"json", "yaml", "JSON", "YAML", "Yaml", " yaml "}
	for _, s := range valid {
		if err := ValidateFormat(s); err != nil {
			t.Errorf("ValidateFormat(%q) = %v, want nil", s, err)
		}
	}

	// the flag defaults to "json", so "" is never the value of an omitted flag —
	// it only happens when someone writes `--output=`, which is a mistake and is
	// rejected like any other unsupported value. (checked separately from the
	// table below because the "error quotes the value" assertion there is vacuous
	// for the empty string.)
	if err := ValidateFormat(""); err == nil {
		t.Error(`ValidateFormat("") = nil, want an error: an explicit --output= is a usage mistake`)
	}

	// `table` never existed as an output format but used to be accepted silently,
	// which is how it ended up documented. It must now be rejected.
	invalid := []string{"table", "tabel", "jsonl", "yml", "xml"}
	for _, s := range invalid {
		err := ValidateFormat(s)
		if err == nil {
			t.Errorf("ValidateFormat(%q) = nil, want an error", s)
			continue
		}
		if !strings.Contains(err.Error(), s) {
			t.Errorf("ValidateFormat(%q) error %q should quote the offending value", s, err)
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
