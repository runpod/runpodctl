package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// firstGraphQLErrorMessage safely extracts the message of the first graphql
// error, tolerating a missing or non-string message rather than panicking on an
// unchecked type assertion.
func firstGraphQLErrorMessage(gqlErrors []interface{}) string {
	const fallback = "unknown graphql error"
	if len(gqlErrors) == 0 {
		return fallback
	}
	first, ok := gqlErrors[0].(map[string]interface{})
	if !ok {
		return fallback
	}
	if msg, ok := first["message"].(string); ok && msg != "" {
		return msg
	}
	return fallback
}

// parseGraphQLData reads a graphql HTTP response and returns its "data" object.
// It returns an error carrying the first graphql error message when the payload
// reports errors, and the raw body so callers can include it in "<field> is nil"
// diagnostics. It always closes the response body.
func parseGraphQLData(res *http.Response) (map[string]interface{}, []byte, error) {
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, nil, fmt.Errorf("statuscode %d", res.StatusCode)
	}

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, nil, err
	}

	data := make(map[string]interface{})
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, rawData, err
	}

	if gqlErrors, ok := data["errors"].([]interface{}); ok && len(gqlErrors) > 0 {
		return nil, rawData, errors.New(firstGraphQLErrorMessage(gqlErrors))
	}

	gqldata, ok := data["data"].(map[string]interface{})
	if !ok || gqldata == nil {
		return nil, rawData, fmt.Errorf("data is nil: %s", string(rawData))
	}
	return gqldata, rawData, nil
}
