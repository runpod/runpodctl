package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/runpod/runpodctl/internal/configenv"
)

// Endpoint represents a serverless endpoint. Its numeric fields carry no
// omitempty: this struct is re-marshalled to render cli output, and 0 is a real
// value for all of them.
type Endpoint struct {
	ID                 string                  `json:"id"`
	Name               string                  `json:"name"`
	TemplateID         string                  `json:"templateId,omitempty"`
	GpuIDs             string                  `json:"gpuIds,omitempty"`     // graphql only (read and write): pool ids with "-<type>" exclusions
	GpuTypeIDs         []string                `json:"gpuTypeIds,omitempty"` // rest read side only: gpu type ids
	InstanceIDs        []string                `json:"instanceIds,omitempty"`
	NetworkVolumeID    string                  `json:"networkVolumeId,omitempty"`
	NetworkVolumeIDs   []EndpointNetworkVolume `json:"networkVolumeIds,omitempty"`
	Locations          string                  `json:"locations,omitempty"`
	IdleTimeout        int                     `json:"idleTimeout"`
	ScalerType         string                  `json:"scalerType,omitempty"`
	ScalerValue        int                     `json:"scalerValue"`
	WorkersMin         int                     `json:"workersMin"`
	WorkersMax         int                     `json:"workersMax"`
	GpuCount           int                     `json:"gpuCount"`
	MinCudaVersion     string                  `json:"minCudaVersion,omitempty"`
	Flashboot          *bool                   `json:"flashboot,omitempty"`
	FlashBootType      string                  `json:"flashBootType,omitempty"`
	ComputeType        string                  `json:"computeType,omitempty"`
	ExecutionTimeoutMs int                     `json:"executionTimeoutMs"`
	ModelReferences    []string                `json:"modelReferences,omitempty"`
	Template           map[string]interface{}  `json:"template,omitempty"`
	Workers            []interface{}           `json:"workers,omitempty"`
	// URLs are the ready-to-call invoke urls, computed client-side from the id so
	// an agent that just created an endpoint can call it without extra lookups.
	URLs *EndpointInvokeURLs `json:"urls,omitempty"`
}

// ServerlessInvokeBaseURL is the default base url used to invoke serverless
// endpoints. Invoke is a separate service from the control plane, so pointing
// runpodctl at a non-prod REST/GraphQL api (RUNPOD_API_URL / RUNPOD_GRAPHQL_URL)
// does not move it; override it explicitly with RUNPOD_INVOKE_URL when testing
// against a non-prod invoke host, otherwise the emitted urls target prod.
const ServerlessInvokeBaseURL = "https://api.runpod.ai/v2"

// EndpointInvokeURLs are the ready-to-call urls for a serverless endpoint.
type EndpointInvokeURLs struct {
	Run     string `json:"run"`
	RunSync string `json:"runsync"`
	Health  string `json:"health"`
}

// invokeBaseURL resolves the base url of the invoke service, honouring the
// RUNPOD_INVOKE_URL / invokeUrl override. It is the single source of the invoke
// host: both the reported urls (invokeURLs) and the client that actually calls
// them (InvokeClient) go through it, so an override can never move one without
// the other.
func invokeBaseURL() string {
	// tolerate a sloppy override: surrounding whitespace and any number of
	// trailing slashes. a value of only slashes is treated as unset rather than
	// silently emitting relative urls.
	base := strings.TrimRight(strings.TrimSpace(configenv.InvokeURL()), "/")
	if base == "" {
		base = ServerlessInvokeBaseURL
	}
	return base
}

// invokeURLs builds the invoke urls for an endpoint id (nil when id is empty).
func invokeURLs(id string) *EndpointInvokeURLs {
	if id == "" {
		return nil
	}
	// escaped for the same reason the invoke client escapes it: these urls are
	// printed for people to curl, and an id with a path-significant character in it
	// would produce a url addressing something else.
	base := invokeBaseURL() + "/" + url.PathEscape(id)
	return &EndpointInvokeURLs{
		Run:     base + "/run",
		RunSync: base + "/runsync",
		Health:  base + "/health",
	}
}

// EndpointNetworkVolume is a multi-region network volume attached to an endpoint.
type EndpointNetworkVolume struct {
	NetworkVolumeID string `json:"networkVolumeId"`
	DataCenterID    string `json:"dataCenterId,omitempty"`
}

// EndpointListResponse is the response from listing endpoints
type EndpointListResponse struct {
	Endpoints []Endpoint `json:"endpoints"`
}

// EndpointUpdateRequest is an endpoint update in the cli's terms; nil and ""
// leave a setting unchanged. UpdateEndpoint maps it onto the v2 body.
type EndpointUpdateRequest struct {
	Name        string
	WorkersMin  *int
	WorkersMax  *int
	IdleTimeout *int
	ScalerType  string
	ScalerValue *int
}

// EndpointListOptions are options for listing endpoints
type EndpointListOptions struct {
	IncludeTemplate bool
	IncludeWorkers  bool
}

// ListEndpoints returns all endpoints, from rest v2.
func (c *Client) ListEndpoints(opts *EndpointListOptions) ([]Endpoint, error) {
	v2Endpoints, err := c.listV2Endpoints()
	if err != nil {
		return nil, err
	}
	// without the catalog, gpuIds would print empty, which reads as "no gpus"
	poolTypes, err := c.gpuPoolTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to read the gpu catalog: %w", err)
	}
	workers, err := c.workerClient(opts)
	if err != nil {
		return nil, err
	}
	endpoints := make([]Endpoint, 0, len(v2Endpoints))
	for i := range v2Endpoints {
		out := v2Endpoints[i].toEndpoint(poolTypes)
		if err := c.decorateEndpoint(&out, &v2Endpoints[i], opts, workers); err != nil {
			return nil, err
		}
		endpoints = append(endpoints, out)
	}
	return endpoints, nil
}

// GetEndpoint returns a single endpoint, from rest v2.
func (c *Client) GetEndpoint(endpointID string, includeTemplate, includeWorkers bool) (*Endpoint, error) {
	e, err := c.getV2Endpoint(endpointID)
	if err != nil {
		return nil, err
	}
	poolTypes, err := c.gpuPoolTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to read the gpu catalog: %w", err)
	}
	opts := &EndpointListOptions{IncludeTemplate: includeTemplate, IncludeWorkers: includeWorkers}
	workers, err := c.workerClient(opts)
	if err != nil {
		return nil, err
	}
	out := e.toEndpoint(poolTypes)
	if err := c.decorateEndpoint(&out, e, opts, workers); err != nil {
		return nil, err
	}
	return &out, nil
}

type v2EndpointUpdate struct {
	Workers map[string]int         `json:"workers,omitempty"`
	Scaling map[string]interface{} `json:"scaling,omitempty"`
}

// UpdateEndpoint updates an endpoint. v2 nests the worker and scaling
// settings, so a change to any of them is sent with the endpoint's current
// values for the rest. the v2 patch goes first, since it is the write that can
// fail validation; the rename (over v1) goes last.
func (c *Client) UpdateEndpoint(endpointID string, req *EndpointUpdateRequest) error {
	body := &v2EndpointUpdate{}
	if req.WorkersMin != nil || req.WorkersMax != nil || req.IdleTimeout != nil || req.ScalerType != "" || req.ScalerValue != nil {
		current, err := c.getV2Endpoint(endpointID)
		if err != nil {
			return err
		}
		if req.WorkersMin != nil || req.WorkersMax != nil || req.IdleTimeout != nil {
			workers := map[string]int{"min": current.Workers.Min, "max": current.Workers.Max}
			if current.Workers.IdleTimeout != nil {
				workers["idleTimeout"] = *current.Workers.IdleTimeout
			}
			if req.WorkersMin != nil {
				workers["min"] = *req.WorkersMin
			}
			if req.WorkersMax != nil {
				workers["max"] = *req.WorkersMax
			}
			if req.IdleTimeout != nil {
				workers["idleTimeout"] = *req.IdleTimeout
			}
			body.Workers = workers
		}
		if req.ScalerType != "" || req.ScalerValue != nil {
			scalerType := current.Scaling.Type
			if req.ScalerType != "" {
				scalerType = strings.ToUpper(req.ScalerType)
			}
			// kept as a float so an unchanged fractional queue delay survives
			value := 0.0
			switch {
			case req.ScalerValue != nil:
				value = float64(*req.ScalerValue)
			case current.Scaling.QueueDelay != nil:
				value = *current.Scaling.QueueDelay
			case current.Scaling.RequestCount != nil:
				value = float64(*current.Scaling.RequestCount)
			}
			body.Scaling = map[string]interface{}{"type": scalerType}
			if scalerType == "REQUEST_COUNT" {
				body.Scaling["requestCount"] = int(math.Round(value))
			} else {
				body.Scaling["queueDelay"] = value
			}
		}
	}
	patched := body.Workers != nil || body.Scaling != nil
	if patched {
		if _, err := c.PatchV2("/serverless/"+url.PathEscape(endpointID), body); err != nil {
			return err
		}
	}
	if req.Name != "" {
		if err := c.renameEndpointV1(endpointID, req.Name); err != nil {
			if patched {
				return fmt.Errorf("endpoint settings were updated, but the rename failed: %w", err)
			}
			return err
		}
	}
	return nil
}

// rp-migrate: keep-v1 start
// renameEndpointV1 renames over rest v1. a v2 rename also renames the
// endpoint's template to "<name>-template", including a template other
// endpoints share; v1 renames only the endpoint.
func (c *Client) renameEndpointV1(endpointID, name string) error {
	_, err := c.Patch("/endpoints/"+url.PathEscape(endpointID), map[string]string{"name": name})
	return err
}

// rp-migrate: keep-v1 end

// rp-migrate: keep-v1 start
// endpoint templates stay on graphql: rest v2 copies a template into a hidden
// per-endpoint template, on create and on a templateId patch alike, so the
// endpoint stops receiving later edits to the template. graphql links it.

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
		return newGraphQLError(resp.Errors[0].Message)
	}

	return nil
}

// rp-migrate: keep-v1 end

// DeleteEndpoint deletes an endpoint
func (c *Client) DeleteEndpoint(endpointID string) error {
	_, err := c.DeleteV2("/serverless/" + url.PathEscape(endpointID))
	return err
}

// rp-migrate: keep-v1 start
// model references have no rest v2 field, so this whole round trip stays on
// graphql.

// gqlEndpointConfig reads the saveEndpoint fields of an endpoint over graphql.
// rest v2 reports no template link, and saveEndpoint is a full replace that
// needs templateId and the gpuIds pool string (with its "-<type>" exclusions)
// back verbatim.
func (c *Client) gqlEndpointConfig(endpointID string) (*Endpoint, error) {
	query := `
		query EndpointConfig($id: String!) {
			myself {
				endpoint(id: $id) {
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
				}
			}
		}
	`

	data, err := c.graphqlRequest(query, map[string]interface{}{"id": endpointID})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Myself *struct {
				Endpoint *Endpoint `json:"endpoint"`
			} `json:"myself"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, newGraphQLError(resp.Errors[0].Message)
	}
	if resp.Data.Myself == nil || resp.Data.Myself.Endpoint == nil {
		return nil, fmt.Errorf("endpoint %s not found", endpointID)
	}
	return resp.Data.Myself.Endpoint, nil
}

// UpdateEndpointModels sets the model references on an existing endpoint via
// saveEndpoint. The full current config is round-tripped so that only
// modelReferences changes — saveEndpoint is a full replace and omitting fields
// would reset them to server defaults. Pass nil or an empty slice to clear all
// model references.
func (c *Client) UpdateEndpointModels(endpointID string, modelRefs []string) (*Endpoint, error) {
	endpoint, err := c.gqlEndpointConfig(endpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch endpoint: %w", err)
	}

	if modelRefs == nil {
		modelRefs = []string{}
	}

	nvIDs := make([]NetworkVolumeIDInput, len(endpoint.NetworkVolumeIDs))
	for i, nv := range endpoint.NetworkVolumeIDs {
		nvIDs[i] = NetworkVolumeIDInput{NetworkVolumeID: nv.NetworkVolumeID}
	}

	// saveEndpoint rejects "" for its FlashBootType enum.
	flashBootType := endpoint.FlashBootType
	if flashBootType == "" {
		flashBootType = "OFF"
	}

	// saveEndpoint rejects an empty gpuIds on anything without instanceIds, so a
	// GPU endpoint that reports none is a read we do not understand — say so
	// instead of writing a config that drops its gpu selection.
	if endpoint.GpuIDs == "" && len(endpoint.InstanceIDs) == 0 {
		return nil, fmt.Errorf("endpoint %s reports no gpuIds and no instanceIds; refusing to update model references with an empty gpu selection", endpointID)
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
			"flashBootType":      flashBootType,
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
		return nil, newGraphQLError(resp.Errors[0].Message)
	}

	if resp.Data.SaveEndpoint == nil {
		return nil, fmt.Errorf("update returned nil response")
	}

	resp.Data.SaveEndpoint.URLs = invokeURLs(resp.Data.SaveEndpoint.ID)

	return resp.Data.SaveEndpoint, nil
}

// rp-migrate: keep-v1 end

// NetworkVolumeIDInput is a single multi-region network volume entry for the
// graphql saveEndpoint mutation (rest uses a flat []string instead).
type NetworkVolumeIDInput struct {
	NetworkVolumeID string `json:"networkVolumeId"`
}

// EndpointCreateGQLInput is the input for creating an endpoint via the graphql
// saveEndpoint mutation. all serverless creates go through this path, so every
// cli flag maps to a field here (the web console uses the same mutation).
type EndpointCreateGQLInput struct {
	Name         string                 `json:"name"`
	HubReleaseID string                 `json:"hubReleaseId,omitempty"`
	TemplateID   string                 `json:"templateId,omitempty"`
	Template     *EndpointTemplateInput `json:"template,omitempty"`
	GpuIDs       string                 `json:"gpuIds,omitempty"`
	// plain int: the api minimum is 1, so 0 is never worth sending.
	GpuCount           int                    `json:"gpuCount,omitempty"`
	InstanceIDs        []string               `json:"instanceIds,omitempty"`
	WorkersMin         *int                   `json:"workersMin,omitempty"`
	WorkersMax         *int                   `json:"workersMax,omitempty"`
	Locations          string                 `json:"locations,omitempty"`
	NetworkVolumeID    string                 `json:"networkVolumeId,omitempty"`
	NetworkVolumeIDs   []NetworkVolumeIDInput `json:"networkVolumeIds,omitempty"`
	IdleTimeout        int                    `json:"idleTimeout,omitempty"`
	ScalerType         string                 `json:"scalerType,omitempty"`
	ScalerValue        int                    `json:"scalerValue,omitempty"`
	ExecutionTimeoutMs *int                   `json:"executionTimeoutMs,omitempty"`
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

// rp-migrate: keep-v1 start
// CreateEndpointGQL creates an endpoint via GraphQL (saveEndpoint mutation).
// rest v2 has no hubReleaseId or modelReferences, and it copies --template-id
// into a hidden template instead of linking it, so creates stay here.
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
		return nil, newGraphQLError(resp.Errors[0].Message)
	}

	if resp.Data.SaveEndpoint == nil {
		return nil, fmt.Errorf("endpoint creation returned nil response")
	}

	resp.Data.SaveEndpoint.URLs = invokeURLs(resp.Data.SaveEndpoint.ID)

	return resp.Data.SaveEndpoint, nil
}

// rp-migrate: keep-v1 end
