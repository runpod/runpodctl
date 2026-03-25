package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Endpoint represents a serverless endpoint
type Endpoint struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	TemplateID      string                 `json:"templateId,omitempty"`
	GpuIDs          string                 `json:"gpuIds,omitempty"`
	NetworkVolumeID string                 `json:"networkVolumeId,omitempty"`
	Locations       string                 `json:"locations,omitempty"`
	IdleTimeout     int                    `json:"idleTimeout,omitempty"`
	ScalerType      string                 `json:"scalerType,omitempty"`
	ScalerValue     int                    `json:"scalerValue,omitempty"`
	WorkersMin      int                    `json:"workersMin,omitempty"`
	WorkersMax      int                    `json:"workersMax,omitempty"`
	GpuCount        int                    `json:"gpuCount,omitempty"`
	Template        map[string]interface{} `json:"template,omitempty"`
	Workers         []interface{}          `json:"workers,omitempty"`
}

// EndpointListResponse is the response from listing endpoints
type EndpointListResponse struct {
	Endpoints []Endpoint `json:"endpoints"`
}

// EndpointCreateRequest is the request to create an endpoint
type EndpointCreateRequest struct {
	Name          string   `json:"name,omitempty"`
	TemplateID    string   `json:"templateId,omitempty"`
	HubReleaseID  string   `json:"hubReleaseId,omitempty"`
	ComputeType   string   `json:"computeType,omitempty"`
	GpuTypeIDs    []string `json:"gpuTypeIds,omitempty"`
	GpuCount      int      `json:"gpuCount,omitempty"`
	WorkersMin    int      `json:"workersMin,omitempty"`
	WorkersMax    int      `json:"workersMax,omitempty"`
	DataCenterIDs    []string `json:"dataCenterIds,omitempty"`
	NetworkVolumeID  string   `json:"networkVolumeId,omitempty"`
}

// EndpointUpdateRequest is the request to update an endpoint
type EndpointUpdateRequest struct {
	Name        string `json:"name,omitempty"`
	WorkersMin  int    `json:"workersMin,omitempty"`
	WorkersMax  int    `json:"workersMax,omitempty"`
	IdleTimeout int    `json:"idleTimeout,omitempty"`
	ScalerType  string `json:"scalerType,omitempty"`
	ScalerValue int    `json:"scalerValue,omitempty"`
}

// EndpointListOptions are options for listing endpoints
type EndpointListOptions struct {
	IncludeTemplate bool
	IncludeWorkers  bool
}

// ListEndpoints returns all endpoints
func (c *Client) ListEndpoints(opts *EndpointListOptions) ([]Endpoint, error) {
	params := url.Values{}
	if opts != nil {
		if opts.IncludeTemplate {
			params.Set("includeTemplate", "true")
		}
		if opts.IncludeWorkers {
			params.Set("includeWorkers", "true")
		}
	}

	data, err := c.Get("/endpoints", params)
	if err != nil {
		return nil, err
	}

	var endpoints []Endpoint
	if err := json.Unmarshal(data, &endpoints); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return endpoints, nil
}

// GetEndpoint returns a single endpoint by ID
func (c *Client) GetEndpoint(endpointID string, includeTemplate, includeWorkers bool) (*Endpoint, error) {
	params := url.Values{}
	if includeTemplate {
		params.Set("includeTemplate", "true")
	}
	if includeWorkers {
		params.Set("includeWorkers", "true")
	}

	data, err := c.Get("/endpoints/"+endpointID, params)
	if err != nil {
		return nil, err
	}

	var endpoint Endpoint
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &endpoint, nil
}

// CreateEndpoint creates a new endpoint
func (c *Client) CreateEndpoint(req *EndpointCreateRequest) (*Endpoint, error) {
	data, err := c.Post("/endpoints", req)
	if err != nil {
		return nil, err
	}

	var endpoint Endpoint
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &endpoint, nil
}

// UpdateEndpoint updates an existing endpoint
func (c *Client) UpdateEndpoint(endpointID string, req *EndpointUpdateRequest) (*Endpoint, error) {
	data, err := c.Patch("/endpoints/"+endpointID, req)
	if err != nil {
		return nil, err
	}

	var endpoint Endpoint
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &endpoint, nil
}

// DeleteEndpoint deletes an endpoint
func (c *Client) DeleteEndpoint(endpointID string) error {
	_, err := c.Delete("/endpoints/" + endpointID)
	return err
}

// EndpointCreateGQLInput is the input for creating an endpoint via GraphQL
// Used when hubReleaseId is needed (REST API doesn't support it)
type EndpointCreateGQLInput struct {
	Name            string                    `json:"name"`
	HubReleaseID    string                    `json:"hubReleaseId,omitempty"`
	TemplateID      string                    `json:"templateId,omitempty"`
	Template        *EndpointTemplateInput    `json:"template,omitempty"`
	GpuIDs          string                    `json:"gpuIds,omitempty"`
	GpuCount        int                       `json:"gpuCount,omitempty"`
	WorkersMin      int                       `json:"workersMin,omitempty"`
	WorkersMax      int                       `json:"workersMax,omitempty"`
	Locations       string                    `json:"locations,omitempty"`
	NetworkVolumeID string                    `json:"networkVolumeId,omitempty"`
}

// EndpointTemplateInput is the inline template for endpoint creation via GraphQL
type EndpointTemplateInput struct {
	Name              string        `json:"name"`
	ImageName         string        `json:"imageName,omitempty"`
	ContainerDiskInGb int           `json:"containerDiskInGb"`
	DockerArgs        string        `json:"dockerArgs"`
	Env               []*PodEnvVar  `json:"env"`
}

// CreateEndpointGQL creates an endpoint via GraphQL (saveEndpoint mutation)
func (c *Client) CreateEndpointGQL(req *EndpointCreateGQLInput) (*Endpoint, error) {
	query := `
		mutation SaveEndpoint($input: EndpointInput!) {
			saveEndpoint(input: $input) {
				id
				name
				gpuIds
				networkVolumeId
				locations
				idleTimeout
				scalerType
				scalerValue
				workersMin
				workersMax
				gpuCount
			}
		}
	`

	variables := map[string]interface{}{
		"input": req,
	}

	data, err := c.graphqlRequest(query, variables)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			SaveEndpoint *Endpoint `json:"saveEndpoint"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
	}

	if resp.Data.SaveEndpoint == nil {
		return nil, fmt.Errorf("endpoint creation returned nil response")
	}

	return resp.Data.SaveEndpoint, nil
}
