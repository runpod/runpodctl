package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/spf13/viper"
)

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
	apiKey := os.Getenv("RUNPOD_API_KEY")
	if apiKey == "" {
		apiKey = viper.GetString("apiKey")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("api key not found. run 'runpod config --apiKey=xxx' or set RUNPOD_API_KEY")
	}

	baseURL := os.Getenv("RUNPOD_API_URL")
	if baseURL == "" {
		baseURL = viper.GetString("restApiUrl")
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	timeout := viper.GetDuration("timeout")
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	userAgent := fmt.Sprintf("runpod-cli/%s (%s; %s)", Version, runtime.GOOS, runtime.GOARCH)

	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
		userAgent:  userAgent,
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
		return nil, fmt.Errorf("api error: %s (status %d)", string(respBody), resp.StatusCode)
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

// APIError represents an error response from the API
type APIError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// FormatError formats an error as JSON for agent consumption
func FormatError(err error) string {
	apiErr := APIError{Error: err.Error()}
	data, _ := json.Marshal(apiErr)
	return string(data)
}
