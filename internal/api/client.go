package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/runpod/runpodctl/internal/agent"
	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

// buildUserAgent constructs the User-Agent string, appending a coding agent
// source tag when the CLI is driven by a recognized AI agent.
func buildUserAgent() string {
	return fmt.Sprintf("runpod-cli/%s (%s; %s)", Version, runtime.GOOS, runtime.GOARCH) + agent.Suffix()
}

const (
	DefaultBaseURL = "https://rest.runpod.io/v1"
	DefaultTimeout = 30 * time.Second
)

var Version string

// Client is the REST API client for runpod
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	userAgent  string
}

// NewClient creates a new REST API client
func NewClient() (*Client, error) {
	apiKey := configenv.APIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("api key not configured. get your key at https://www.runpod.io/console/user/settings then: export RUNPOD_API_KEY=your-key OR run: runpodctl doctor")
	}

	baseURL := configenv.RESTURL()
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	timeout := viper.GetDuration("timeout")
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
		userAgent:  buildUserAgent(),
	}, nil
}

// request makes an HTTP request to the API
func (c *Client) request(method, endpoint string, params url.Values, body interface{}) ([]byte, error) {
	u := c.baseURL + endpoint
	if params != nil && len(params) > 0 {
		u += "?" + params.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequest(method, u, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(respBody, resp.StatusCode)
	}

	return respBody, nil
}

// Get makes a GET request
func (c *Client) Get(endpoint string, params url.Values) ([]byte, error) {
	return c.request(http.MethodGet, endpoint, params, nil)
}

// Post makes a POST request
func (c *Client) Post(endpoint string, body interface{}) ([]byte, error) {
	return c.request(http.MethodPost, endpoint, nil, body)
}

// Patch makes a PATCH request
func (c *Client) Patch(endpoint string, body interface{}) ([]byte, error) {
	return c.request(http.MethodPatch, endpoint, nil, body)
}

// Delete makes a DELETE request
func (c *Client) Delete(endpoint string) ([]byte, error) {
	return c.request(http.MethodDelete, endpoint, nil, nil)
}

// APIError is a structured error returned by the runpod API. It implements the
// error interface and exposes a stable machine-readable code plus the HTTP
// status so the cli can emit a single flat JSON error object for agents.
type APIError struct {
	Message string `json:"error"`
	Code    string `json:"code,omitempty"`
	Status  int    `json:"status,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string { return e.Message }

// ErrorCode returns a stable machine-readable code for the error, deriving one
// from the HTTP status when the API did not supply an explicit code.
func (e *APIError) ErrorCode() string {
	if e.Code != "" {
		return e.Code
	}
	return codeForStatus(e.Status)
}

// HTTPStatus returns the HTTP status code associated with the error (0 if none).
func (e *APIError) HTTPStatus() int { return e.Status }

// codeForStatus maps an HTTP status to a stable, lowercase error code.
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "rate_limited"
	}
	switch {
	case status >= 500:
		return "server_error"
	case status != 0:
		return "api_error"
	default:
		return ""
	}
}

// parseAPIError builds a structured APIError from a non-2xx response body. It
// unwraps the common {"error": "..."} / {"message": "..."} envelope so the
// message is not double-encoded (e.g. `api error: {"error":"pod not found"}`),
// and preserves any explicit code the API returned.
func parseAPIError(body []byte, status int) *APIError {
	apiErr := &APIError{Status: status}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var envelope struct {
			Error   string `json:"error"`
			Message string `json:"message"`
			Code    string `json:"code"`
		}
		if err := json.Unmarshal(trimmed, &envelope); err == nil {
			switch {
			case envelope.Error != "":
				apiErr.Message = envelope.Error
			case envelope.Message != "":
				apiErr.Message = envelope.Message
			}
			apiErr.Code = envelope.Code
		}
	}

	if apiErr.Message == "" {
		apiErr.Message = strings.TrimSpace(string(body))
	}
	if apiErr.Message == "" {
		apiErr.Message = fmt.Sprintf("api request failed with status %d", status)
	}

	return apiErr
}

// FormatError formats an error as a flat JSON object for agent consumption.
func FormatError(err error) string {
	apiErr := APIError{Message: err.Error()}
	data, _ := json.Marshal(apiErr)
	return string(data)
}
