package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	runpod "github.com/runpod/go-sdk/v2"
	"github.com/spf13/viper"

	"github.com/runpod/runpodctl/internal/configenv"
)

// V2Client serves the v2 calls that need a per-call context: the worker
// listing (the log streams have their own client in logs.go). Ordinary v2
// requests go through Client's SDK client (see sdk.go).

// V2Client makes ordinary (non-streaming) json requests against the v2 rest api.
type V2Client struct {
	baseURL string
	sdk     *runpod.Client
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

	baseURL := restBaseURL()
	sdk, err := newSDK(baseURL, apiKey, buildUserAgent(), timeout)
	if err != nil {
		return nil, err
	}
	return &V2Client{baseURL: baseURL, sdk: sdk}, nil
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
	if err := requireID(endpointID); err != nil {
		return nil, err
	}
	// BACKWARD_COMPATIBILITY: decoded into the cli's own types, which match the
	// wire shape: the SDK's workers parse startedAt into time.Time, which would
	// print it differently from what the API sent. Next major: decode into the
	// SDK's workers.
	body, err := sdkSendContext(ctx, c.sdk, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Serverless.ListEndpointWorkers(ctx, endpointID)
	})
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
