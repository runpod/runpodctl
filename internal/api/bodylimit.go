package api

import (
	"fmt"
	"io"
	"strings"

	"github.com/runpod/runpodctl/internal/redact"
)

// MaxResponseBytes bounds how much of an api response body the cli buffers. It
// is far above any real response (the largest are model and pod listings, and
// serverless sync output, which the platform caps near 20 MB); it exists so a
// hostile or broken server cannot make the cli hold hundreds of MB.
const MaxResponseBytes = 32 << 20

// ReadBounded reads body up to MaxResponseBytes and fails if the server sent
// more, rather than silently truncating into invalid json.
func ReadBounded(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, MaxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxResponseBytes {
		return nil, fmt.Errorf("response exceeds %d MiB limit", MaxResponseBytes>>20)
	}
	return data, nil
}

// unreadableBodyError is the error for a rejected (non-2xx) response whose body
// ReadBounded refused (oversized) or could not finish (cut short). The response
// itself was received, so the error keeps its status and stable code rather
// than the bare read error -- but the (redacted) read error stays reachable
// through Unwrap, so a transient network failure behind the body still
// classifies as one for classifiers that walk the chain (isNetworkError). The
// received status sets the emitted code: output.Error prefers ErrorCode()
// (server_error for a 502) over the network fallback, so the JSON contract for
// agents is unchanged by the preserved cause.
func unreadableBodyError(status int, readErr error) *APIError {
	cause := redact.Error(readErr)
	return &APIError{
		Status:  status,
		Code:    codeForStatus(status),
		Message: fmt.Sprintf("api request failed with status %d (the error body could not be read: %v)", status, cause),
		cause:   cause,
	}
}

// ReadBodyForStatus is ReadBounded for a response with a known status. A read
// failure on a non-2xx response is an APIError carrying that status and its
// stable code; on a 2xx response it stays the plain read error, since nothing
// was rejected.
func ReadBodyForStatus(status int, body io.Reader) ([]byte, error) {
	data, err := ReadBounded(body)
	if err != nil {
		if status < 200 || status >= 300 {
			return nil, unreadableBodyError(status, err)
		}
		return nil, err
	}
	return data, nil
}

// ErrorBodyExcerptBytes bounds how much of a response body is echoed into an
// error message. The body itself may be up to MaxResponseBytes, which is far too
// much to print.
const ErrorBodyExcerptBytes = 4 << 10

// errorBodyRedactWindow is how much of a body is run through redaction. It is
// larger than the excerpt so a secret cut by the excerpt boundary has already
// been redacted whole, while redaction never scans a multi-megabyte body.
const errorBodyRedactWindow = 64 << 10

// ErrorBodyExcerpt renders a response body for an error message: redacted (the
// same redact.Text the model-repo errors use), then cut at ErrorBodyExcerptBytes
// with a marker. Use it for every "<field> is nil: <body>" style message.
func ErrorBodyExcerpt(body []byte) string {
	window := body
	if len(window) > errorBodyRedactWindow {
		window = window[:errorBodyRedactWindow]
	}
	text := redact.Text(strings.ToValidUTF8(string(window), ""))
	truncated := len(window) < len(body)
	if len(text) > ErrorBodyExcerptBytes {
		text = strings.ToValidUTF8(text[:ErrorBodyExcerptBytes], "")
		truncated = true
	}
	if truncated {
		text += "... (truncated)"
	}
	return text
}

// ErrorMessageExcerpt bounds and redacts a message extracted from an error
// response (an "error"/"message"/"detail" field, an errors[] entry or a graphql
// error message). The envelope can be valid JSON of tens of megabytes with a
// presigned URL inside one field, so extraction does not make the text safe to
// print on its own.
func ErrorMessageExcerpt(msg string) string {
	return ErrorBodyExcerpt([]byte(msg))
}
