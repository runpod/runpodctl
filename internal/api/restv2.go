package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

// The v2 rest api is a separate host from the v1 control plane this cli does its
// crud against (see configenv.RESTV2URL). Only the two features that do not
// exist on v1 live here: the log streams (logs.go) and the worker listing.
// Everything else must keep using Client, so that one api version change does not
// silently move unrelated commands.

// restV2BaseURL resolves the v2 base url, falling back to prod.
func restV2BaseURL() string {
	baseURL := configenv.RESTV2URL()
	if baseURL == "" {
		baseURL = DefaultRESTV2BaseURL
	}
	return strings.TrimSuffix(baseURL, "/")
}

// V2Client makes ordinary (non-streaming) json requests against the v2 rest api.
type V2Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	userAgent  string
}

// NewV2Client creates a client for the v2 rest api.
func NewV2Client() (*V2Client, error) {
	apiKey := configenv.APIKey()
	if apiKey == "" {
		return nil, ErrNoCredentials
	}

	timeout := viper.GetDuration("timeout")
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return &V2Client{
		baseURL:    restV2BaseURL(),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
		userAgent:  buildUserAgent(),
	}, nil
}

// get issues a GET against the v2 api and returns the raw body.
func (c *V2Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(body, resp.StatusCode)
	}
	return body, nil
}

// Worker is one worker backing a serverless endpoint, as reported by v2.
//
// Status is the value the v2 worker listing reports (INITIALIZING, IDLE,
// RUNNING, THROTTLED, UNHEALTHY). This is a different and more truthful signal
// than the v1 `includeWorkers` expansion, which reports desiredStatus EXITED for
// every worker on a warm healthy endpoint -- see the note in cmd/serverless.
type Worker struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Image        string `json:"image,omitempty"`
	Version      int    `json:"version,omitempty"`
	GPUCount     int    `json:"gpuCount,omitempty"`
	GPUTypeID    string `json:"gpuTypeId,omitempty"`
	DataCenterID string `json:"dataCenterId,omitempty"`
	StartedAt    string `json:"startedAt,omitempty"`
	IsStale      bool   `json:"isStale,omitempty"`
}

// WorkerSummary is the aggregate worker count by state.
type WorkerSummary struct {
	Total        int `json:"total"`
	Idle         int `json:"idle"`
	Initializing int `json:"initializing"`
	Running      int `json:"running"`
	Throttled    int `json:"throttled"`
	Unhealthy    int `json:"unhealthy"`
}

// WorkersResponse is the v2 worker listing.
type WorkersResponse struct {
	Workers         []Worker      `json:"workers"`
	Summary         WorkerSummary `json:"summary"`
	EndpointVersion int           `json:"endpointVersion,omitempty"`
}

// ListEndpointWorkers reads the workers backing an endpoint.
//
// This is the only usable source of worker ids for `serverless logs`: the log
// route addresses a worker directly, and the v1 endpoint expansion cannot be
// trusted to say which workers exist or what state they are in.
func (c *V2Client) ListEndpointWorkers(ctx context.Context, endpointID string) (*WorkersResponse, error) {
	body, err := c.get(ctx, "/serverless/"+url.PathEscape(endpointID)+"/workers")
	if err != nil {
		return nil, err
	}
	var workers WorkersResponse
	if err := json.Unmarshal(body, &workers); err != nil {
		return nil, fmt.Errorf("failed to parse workers response: %w", err)
	}
	return &workers, nil
}

// ListEndpointWorkersWithTimeout is ListEndpointWorkers with the standard
// per-call deadline, for callers that have no context of their own.
func (c *V2Client) ListEndpointWorkersWithTimeout(endpointID string) (*WorkersResponse, error) {
	timeout := viper.GetDuration("timeout")
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.ListEndpointWorkers(ctx, endpointID)
}
