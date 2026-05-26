package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Endpoint represents a serverless endpoint
type Endpoint struct {
	ID                 string                  `json:"id"`
	Name               string                  `json:"name"`
	TemplateID         string                  `json:"templateId,omitempty"`
	GpuIDs             string                  `json:"gpuIds,omitempty"`
	InstanceIDs        []string                `json:"instanceIds,omitempty"`
	NetworkVolumeID    string                  `json:"networkVolumeId,omitempty"`
	NetworkVolumeIDs   []EndpointNetworkVolume `json:"networkVolumeIds,omitempty"`
	Locations          string                  `json:"locations,omitempty"`
	IdleTimeout        int                     `json:"idleTimeout,omitempty"`
	ScalerType         string                  `json:"scalerType,omitempty"`
	ScalerValue        int                     `json:"scalerValue,omitempty"`
	WorkersMin         int                     `json:"workersMin,omitempty"`
	WorkersMax         int                     `json:"workersMax,omitempty"`
	GpuCount           int                     `json:"gpuCount,omitempty"`
	MinCudaVersion     string                  `json:"minCudaVersion,omitempty"`
	Flashboot          *bool                   `json:"flashboot,omitempty"`
	FlashBootType      string                  `json:"flashBootType,omitempty"`
	ComputeType        string                  `json:"computeType,omitempty"`
	ExecutionTimeoutMs int                     `json:"executionTimeoutMs,omitempty"`
	ModelReferences    []string                `json:"modelReferences,omitempty"`
	Template           map[string]interface{}  `json:"template,omitempty"`
	Workers            []interface{}           `json:"workers,omitempty"`
}

// EndpointNetworkVolume is a multi-region network volume attached to an endpoint.
type EndpointNetworkVolume struct {
	NetworkVolumeID string `json:"networkVolumeId"`
	DataCenterID    string `json:"dataCenterId,omitempty"`
}

// UnmarshalJSON tolerates both shapes of networkVolumeIds: the rest read
// endpoint returns bare id strings (["vol-1"]) while the graphql saveEndpoint
// write path uses objects ([{"networkVolumeId":"vol-1"}]).
func (v *EndpointNetworkVolume) UnmarshalJSON(data []byte) error {
	var id string
	if err := json.Unmarshal(data, &id); err == nil {
		v.NetworkVolumeID = id
		return nil
	}

	type alias EndpointNetworkVolume
	var obj alias
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*v = EndpointNetworkVolume(obj)
	return nil
}

// EndpointListResponse is the response from listing endpoints
type EndpointListResponse struct {
	Endpoints []Endpoint `json:"endpoints"`
}

// EndpointUpdateRequest is the request to update an endpoint
type EndpointUpdateRequest struct {
	Name        string `json:"name,omitempty"`
	WorkersMin  int    `json:"workersMin,omitempty"`
	WorkersMax  int    `json:"workersMax,omitempty"`
	IdleTimeout int    `json:"idleTimeout,omitempty"`
	ScalerType  string `json:"scalerType,omitempty"`
	ScalerValue int    `json:"scalerValue,omitempty"`
	Flashboot   *bool  `json:"flashboot,omitempty"`
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

// UpdateEndpointTemplate updates the template attached to an endpoint via GraphQL.
func (c *Client) UpdateEndpointTemplate(endpointID, templateID string) error {
	query := `
		mutation Mutation($input: UpdateEndpointTemplateInput) {
			updateEndpointTemplate(input: $input) {
			  id
			  templateId
			}
		  }
	`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"endpointId": endpointID,
			"templateId": templateID,
		},
	}

	data, err := c.graphqlRequest(query, variables)
	if err != nil {
		return err
	}

	var resp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
	}

	return nil
}

// DeleteEndpoint deletes an endpoint
func (c *Client) DeleteEndpoint(endpointID string) error {
	_, err := c.Delete("/endpoints/" + endpointID)
	return err
}

// UpdateEndpointModels sets the model references on an existing endpoint via
// saveEndpoint. The full current config is round-tripped so that only
// modelReferences changes — saveEndpoint is a full replace and omitting fields
// would reset them to server defaults. Pass nil or an empty slice to clear all
// model references.
func (c *Client) UpdateEndpointModels(endpointID string, modelRefs []string) (*Endpoint, error) {
	endpoint, err := c.GetEndpoint(endpointID, false, false)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch endpoint: %w", err)
	}

	if modelRefs == nil {
		modelRefs = []string{}
	}

	// saveEndpoint expects networkVolumeIds as [{networkVolumeId}] objects; the
	// REST read shape uses bare id strings which UnmarshalJSON normalises into
	// EndpointNetworkVolume — convert to the GraphQL write shape here.
	nvIDs := make([]NetworkVolumeIDInput, len(endpoint.NetworkVolumeIDs))
	for i, nv := range endpoint.NetworkVolumeIDs {
		nvIDs[i] = NetworkVolumeIDInput{NetworkVolumeID: nv.NetworkVolumeID}
	}

	query := `
		mutation SaveEndpoint($input: EndpointInput!) {
			saveEndpoint(input: $input) {
				id
				name
				templateId
				gpuIds
				gpuCount
				instanceIds
				workersMin
				workersMax
				locations
				networkVolumeId
				networkVolumeIds { networkVolumeId }
				idleTimeout
				scalerType
				scalerValue
				executionTimeoutMs
				minCudaVersion
				flashBootType
				modelReferences
			}
		}
	`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"id":                 endpointID,
			"name":               endpoint.Name,
			"templateId":         endpoint.TemplateID,
			"gpuIds":             endpoint.GpuIDs,
			"gpuCount":           endpoint.GpuCount,
			"instanceIds":        endpoint.InstanceIDs,
			"workersMin":         endpoint.WorkersMin,
			"workersMax":         endpoint.WorkersMax,
			"locations":          endpoint.Locations,
			"networkVolumeId":    endpoint.NetworkVolumeID,
			"networkVolumeIds":   nvIDs,
			"idleTimeout":        endpoint.IdleTimeout,
			"scalerType":         endpoint.ScalerType,
			"scalerValue":        endpoint.ScalerValue,
			"executionTimeoutMs": endpoint.ExecutionTimeoutMs,
			"minCudaVersion":     endpoint.MinCudaVersion,
			"flashBootType":      endpoint.FlashBootType,
			"modelReferences":    modelRefs,
		},
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
		return nil, fmt.Errorf("update returned nil response")
	}

	return resp.Data.SaveEndpoint, nil
}

// NetworkVolumeIDInput is a single multi-region network volume entry for the
// graphql saveEndpoint mutation (rest uses a flat []string instead).
type NetworkVolumeIDInput struct {
	NetworkVolumeID string `json:"networkVolumeId"`
}

// EndpointCreateGQLInput is the input for creating an endpoint via the graphql
// saveEndpoint mutation. all serverless creates go through this path, so every
// cli flag maps to a field here (the web console uses the same mutation).
type EndpointCreateGQLInput struct {
	Name               string                 `json:"name"`
	HubReleaseID       string                 `json:"hubReleaseId,omitempty"`
	TemplateID         string                 `json:"templateId,omitempty"`
	Template           *EndpointTemplateInput `json:"template,omitempty"`
	GpuIDs             string                 `json:"gpuIds,omitempty"`
	GpuCount           int                    `json:"gpuCount,omitempty"`
	InstanceIDs        []string               `json:"instanceIds,omitempty"`
	WorkersMin         int                    `json:"workersMin,omitempty"`
	WorkersMax         int                    `json:"workersMax,omitempty"`
	Locations          string                 `json:"locations,omitempty"`
	NetworkVolumeID    string                 `json:"networkVolumeId,omitempty"`
	NetworkVolumeIDs   []NetworkVolumeIDInput `json:"networkVolumeIds,omitempty"`
	IdleTimeout        int                    `json:"idleTimeout,omitempty"`
	ScalerType         string                 `json:"scalerType,omitempty"`
	ScalerValue        int                    `json:"scalerValue,omitempty"`
	ExecutionTimeoutMs int                    `json:"executionTimeoutMs,omitempty"`
	MinCudaVersion     string                 `json:"minCudaVersion,omitempty"`
	FlashBootType      string                 `json:"flashBootType,omitempty"`
	ModelReferences    []string               `json:"modelReferences,omitempty"`
}

// EndpointTemplateInput is the inline template for endpoint creation via GraphQL
type EndpointTemplateInput struct {
	Name              string       `json:"name"`
	ImageName         string       `json:"imageName,omitempty"`
	ContainerDiskInGb int          `json:"containerDiskInGb"`
	DockerArgs        string       `json:"dockerArgs"`
	Env               []*PodEnvVar `json:"env"`
}

// CreateEndpointGQL creates an endpoint via GraphQL (saveEndpoint mutation)
func (c *Client) CreateEndpointGQL(req *EndpointCreateGQLInput) (*Endpoint, error) {
	query := `
		mutation SaveEndpoint($input: EndpointInput!) {
			saveEndpoint(input: $input) {
				id
				name
				templateId
				gpuIds
				instanceIds
				computeType
				networkVolumeId
				networkVolumeIds {
					networkVolumeId
					dataCenterId
				}
				locations
				idleTimeout
				scalerType
				scalerValue
				workersMin
				workersMax
				gpuCount
				minCudaVersion
				executionTimeoutMs
				flashBootType
				modelReferences
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
