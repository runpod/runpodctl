package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	internalapi "github.com/runpod/runpodctl/internal/api"
)

// the model, pod, volume and endpoint helpers read the body through
// parseGraphQLData or readResponseBody: a rejected status with an oversized body
// keeps its status and stable code, a 200 with one stays a plain read error.
func TestGraphQLHelpersKeepStatusOnUnreadableBody(t *testing.T) {
	oversized := strings.Repeat("a", maxModelResponseBytes+1)
	for status, wantCode := range map[int]string{http.StatusTooManyRequests: "rate_limited", http.StatusServiceUnavailable: "server_error"} {
		_, _, err := parseGraphQLData(status, strings.NewReader(oversized))
		var apiErr *internalapi.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("status %d: expected an *APIError, got %T: %v", status, err, err)
		}
		if apiErr.Status != status || apiErr.ErrorCode() != wantCode {
			t.Fatalf("status %d: got status %d code %q, want code %q", status, apiErr.Status, apiErr.ErrorCode(), wantCode)
		}

		res := &http.Response{StatusCode: status, Body: http.NoBody}
		res.Body = readerCloser{strings.NewReader(oversized)}
		if _, err := readResponseBody(res); !errors.As(err, &apiErr) || apiErr.Status != status {
			t.Fatalf("status %d: readResponseBody error = %v", status, err)
		}
	}

	_, _, err := parseGraphQLData(http.StatusOK, strings.NewReader(oversized))
	var apiErr *internalapi.APIError
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("a 200 read failure must be a plain error, got %T: %v", err, err)
	}
}

type readerCloser struct{ *strings.Reader }

func (readerCloser) Close() error { return nil }
