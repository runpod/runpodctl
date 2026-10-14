package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/runpod/runpodctl/internal/configenv"
)

// Endpoint represents a serverless endpoint. Its numeric fields carry no
// omitempty: this struct is re-marshalled to render cli output, and 0 is a real
// value for all of them.
type Endpoint struct {
	ID               string                  `json:"id"`
	Name             string                  `json:"name"`
	TemplateID       string                  `json:"templateId,omitempty"`
	GpuIDs           string                  `json:"gpuIds,omitempty"`     // graphql only (read and write): pool ids with "-<type>" exclusions
	GpuTypeIDs       []string                `json:"gpuTypeIds,omitempty"` // reads: gpu type ids, expanded from v2's pools
	InstanceIDs      []string                `json:"instanceIds,omitempty"`
	NetworkVolumeID  string                  `json:"networkVolumeId,omitempty"`
	NetworkVolumeIDs []EndpointNetworkVolume `json:"networkVolumeIds,omitempty"`
	Locations        string                  `json:"locations,omitempty"`
	// IdleTimeout is nil only when neither v2 nor v1 reported one (an endpoint
	// created or deleted between the two reads of a list)
	IdleTimeout        *int                   `json:"idleTimeout,omitempty"`
	ScalerType         string                 `json:"scalerType,omitempty"`
	ScalerValue        float64                `json:"scalerValue"` // a queue delay is fractional (minimum 0.5)
	WorkersMin         int                    `json:"workersMin"`
	WorkersMax         int                    `json:"workersMax"`
	GpuCount           int                    `json:"gpuCount"`
	MinCudaVersion     string                 `json:"minCudaVersion,omitempty"`
	Flashboot          *bool                  `json:"flashboot,omitempty"`
	FlashBootType      string                 `json:"flashBootType,omitempty"`
	ComputeType        string                 `json:"computeType,omitempty"`
	ExecutionTimeoutMs int                    `json:"executionTimeoutMs"`
	ModelReferences    []string               `json:"modelReferences,omitempty"`
	Template           map[string]interface{} `json:"template,omitempty"`
	Workers            []interface{}          `json:"workers,omitempty"`
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

// EndpointUpdateRequest is an endpoint update, in rest v1's wire shape; nil and
// "" leave a setting unchanged.
type EndpointUpdateRequest struct {
	Name        string `json:"name,omitempty"`
	WorkersMin  *int   `json:"workersMin,omitempty"`
	WorkersMax  *int   `json:"workersMax,omitempty"`
	IdleTimeout *int   `json:"idleTimeout,omitempty"`
	ScalerType  string `json:"scalerType,omitempty"`
	ScalerValue *int   `json:"scalerValue,omitempty"`
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
	poolTypes, err := c.poolTypesFor(v2Endpoints)
	if err != nil {
		return nil, err
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
	if err := c.fillIdleTimeouts(endpoints, v2Endpoints, true); err != nil {
		return nil, err
	}
	return endpoints, nil
}

// poolTypesFor reads the gpu catalog, which maps v2's pools to gpu types, when
// any endpoint has a gpu block. without it gpuIds would print empty, which
// reads as "no gpus"; cpu endpoints don't need it.
func (c *Client) poolTypesFor(endpoints []v2Endpoint) (map[string][]string, error) {
	for i := range endpoints {
		if endpoints[i].Gpu != nil {
			poolTypes, err := c.gpuPoolTypes()
			if err != nil {
				return nil, fmt.Errorf("failed to read the gpu catalog: %w", err)
			}
			return poolTypes, nil
		}
	}
	return nil, nil
}

// fillIdleTimeouts reads, over rest v1, the idle timeouts v2 leaves out (see
// v1IdleTimeouts). out[i] is the mapping of v2[i]. a failed read is an error:
// 0 would read as a real setting. an endpoint v1 doesn't list is an error too,
// unless skipUnlisted: a list then leaves that row's idleTimeout out rather
// than failing on an endpoint created or deleted between the two reads.
func (c *Client) fillIdleTimeouts(out []Endpoint, v2 []v2Endpoint, skipUnlisted bool) error {
	var missing []string
	for i := range v2 {
		if v2[i].Workers.IdleTimeout == nil {
			missing = append(missing, v2[i].ID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	idle, err := c.v1IdleTimeouts(missing)
	if err != nil {
		// %v, not %w: a v1 404 must not make an endpoint v2 just returned
		// read as not_found
		return fmt.Errorf("failed to read the idle timeout: %v", err)
	}
	for i := range out {
		if v2[i].Workers.IdleTimeout == nil {
			value, ok := idle[out[i].ID]
			if !ok {
				if skipUnlisted {
					continue
				}
				return fmt.Errorf("failed to read the idle timeout: rest v1 did not report endpoint %s", out[i].ID)
			}
			out[i].IdleTimeout = &value
		}
	}
	return nil
}

// GetEndpoint returns a single endpoint, from rest v2.
func (c *Client) GetEndpoint(endpointID string, includeTemplate, includeWorkers bool) (*Endpoint, error) {
	e, err := c.getV2Endpoint(endpointID)
	if err != nil {
		return nil, err
	}
	poolTypes, err := c.poolTypesFor([]v2Endpoint{*e})
	if err != nil {
		return nil, err
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
	outs := []Endpoint{out}
	if err := c.fillIdleTimeouts(outs, []v2Endpoint{*e}, false); err != nil {
		return nil, err
	}
	return &outs[0], nil
}

// rp-migrate: keep-v1 start
// UpdateEndpoint updates an endpoint over rest v1, in one patch. every v2 patch
// of an endpoint, even one that only changes workers, also renames the
// endpoint's template to "<endpoint name>-template", including a template
// other endpoints share; v1 leaves the template alone.
func (c *Client) UpdateEndpoint(endpointID string, req *EndpointUpdateRequest) error {
	_, err := c.Patch("/endpoints/"+url.PathEscape(endpointID), req)
	return err
}

// v1IdleTimeouts reads idle timeouts over rest v1, keyed by endpoint id. v2
// leaves workers.idleTimeout out on request-count endpoints, though the
// endpoint still has one.
func (c *Client) v1IdleTimeouts(endpointIDs []string) (map[string]int, error) {
	type v1Endpoint struct {
		ID          string `json:"id"`
		IdleTimeout int    `json:"idleTimeout"`
	}
	var endpoints []v1Endpoint
	if len(endpointIDs) == 1 {
		data, err := c.Get("/endpoints/"+url.PathEscape(endpointIDs[0]), nil)
		if err != nil {
			return nil, err
		}
		var e v1Endpoint
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		endpoints = append(endpoints, e)
	} else {
		data, err := c.Get("/endpoints", nil)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &endpoints); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
	}
	out := make(map[string]int, len(endpoints))
	for _, e := range endpoints {
		out[e.ID] = e.IdleTimeout
	}
	return out, nil
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
		return nil, NewNotFoundError("endpoint %s not found", endpointID)
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
