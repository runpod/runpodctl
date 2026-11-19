// Package api wraps the RunPod GraphQL and REST endpoints used by the CLI.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	internalapi "github.com/runpod/runpodctl/internal/api"
)

// errorBodyExcerptBytes is the bound errorBodyExcerpt enforces.
const errorBodyExcerptBytes = internalapi.ErrorBodyExcerptBytes

// errorBodyExcerpt is body, redacted and cut at errorBodyExcerptBytes, for an
// error message. See internalapi.ErrorBodyExcerpt.
func errorBodyExcerpt(body []byte) string {
	return internalapi.ErrorBodyExcerpt(body)
}

// firstGraphQLErrorMessage safely extracts the message of the first graphql
// error, tolerating a missing or non-string message rather than panicking on an
// unchecked type assertion.
func firstGraphQLErrorMessage(gqlErrors []any) string {
	const fallback = "unknown graphql error"
	if len(gqlErrors) == 0 {
		return fallback
	}
	first, ok := gqlErrors[0].(map[string]any)
	if !ok {
		return fallback
	}
	if msg, ok := first["message"].(string); ok && msg != "" {
		return msg
	}
	return fallback
}

// queryGraphQLData sends input and returns the response's "data" object. It owns
// the response lifecycle so callers never hold an open body.
func queryGraphQLData(input Input) (gqldata map[string]any, rawData []byte, err error) {
	res, err := Query(input)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		closeErr := res.Body.Close()
		if err == nil && closeErr != nil {
			gqldata, err = nil, fmt.Errorf("close graphql response body: %w", closeErr)
		}
	}()
	return parseGraphQLData(res.StatusCode, res.Body)
}

// parseGraphQLData decodes a graphql HTTP response and returns its "data" object.
// It returns an error carrying the first graphql error message when the payload
// reports errors, or the status code and body for non-200 responses. The raw
// body (bounded by maxModelResponseBytes; a larger response is an error) is returned
// so callers can include it in "<field> is nil" diagnostics.
func parseGraphQLData(status int, body io.Reader) (gqldata map[string]any, rawData []byte, err error) {
	rawData, err = ReadBoundedResponse(body)
	if err != nil {
		return nil, nil, err
	}

	if status != http.StatusOK {
		return nil, rawData, fmt.Errorf("statuscode %d: %s", status, errorBodyExcerpt(rawData))
	}

	data := make(map[string]any)
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, rawData, err
	}

	if gqlErrors, ok := data["errors"].([]any); ok && len(gqlErrors) > 0 {
		return nil, rawData, errors.New(firstGraphQLErrorMessage(gqlErrors))
	}

	gqldata, ok := data["data"].(map[string]any)
	if !ok || gqldata == nil {
		return nil, rawData, fmt.Errorf("data is nil: %s", errorBodyExcerpt(rawData))
	}
	return gqldata, rawData, nil
}

// maxModelResponseBytes is the bound ReadBoundedResponse enforces.
const maxModelResponseBytes = internalapi.MaxResponseBytes

// ReadBoundedResponse reads a graphql response body up to maxModelResponseBytes
// and fails if the server sent more, rather than buffering hundreds of MB (a
// 300 MB body measured ~800 MB RSS) or silently truncating into invalid json.
func ReadBoundedResponse(body io.Reader) ([]byte, error) {
	return internalapi.ReadBounded(body)
}
