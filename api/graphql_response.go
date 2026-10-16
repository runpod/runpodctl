// Package api wraps the RunPod GraphQL and REST endpoints used by the CLI.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// maxGraphQLBodyBytes bounds how much of a response is read, so a misbehaving
// server cannot make the CLI buffer an unbounded body.
const maxGraphQLBodyBytes = 64 << 10

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
// body (bounded by maxGraphQLBodyBytes) is returned so callers can include it in
// "<field> is nil" diagnostics.
func parseGraphQLData(status int, body io.Reader) (gqldata map[string]any, rawData []byte, err error) {
	rawData, err = io.ReadAll(io.LimitReader(body, maxGraphQLBodyBytes))
	if err != nil {
		return nil, nil, err
	}

	if status != http.StatusOK {
		return nil, rawData, fmt.Errorf("statuscode %d: %s", status, string(rawData))
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
		return nil, rawData, fmt.Errorf("data is nil: %s", string(rawData))
	}
	return gqldata, rawData, nil
}
