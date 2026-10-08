package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
)

// v2Endpoint is a serverless endpoint as rest v2 reports it. toEndpoint maps it
// onto the field names the cli has always printed. v2 keeps no link to a
// template, so templateId is not reported.
type v2Endpoint struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Args     string            `json:"args"`
	Cmd      []string          `json:"cmd"`
	Env      map[string]string `json:"env"`
	Ports    []string          `json:"ports"`
	Disk     int               `json:"disk"`
	Registry *string           `json:"registry"`
	Gpu      *struct {
		Pools          []string `json:"pools"`
		ExcludedTypes  []string `json:"excludedTypes"`
		Count          int      `json:"count"`
		MinCudaVersion string   `json:"minCudaVersion"`
	} `json:"gpu"`
	// cpu endpoints report a list of cpu configs; the cli never printed them
	Cpu            json.RawMessage `json:"cpu"`
	NetworkVolumes []string        `json:"networkVolumes"`
	DataCenterIDs  []string        `json:"dataCenterIds"`
	Flashboot      string          `json:"flashboot"`
	Timeout        int             `json:"timeout"`
	Scaling        struct {
		Type         string   `json:"type"`
		QueueDelay   *float64 `json:"queueDelay"` // fractional seconds, minimum 0.5
		RequestCount *int     `json:"requestCount"`
	} `json:"scaling"`
	Workers struct {
		Min         int  `json:"min"`
		Max         int  `json:"max"`
		IdleTimeout *int `json:"idleTimeout"`
	} `json:"workers"`
}

// toEndpoint maps the endpoint; poolTypes expands pools into the gpu type ids
// v1 reported (nil skips that).
func (e *v2Endpoint) toEndpoint(poolTypes map[string][]string) Endpoint {
	out := Endpoint{
		ID:                 e.ID,
		Name:               e.Name,
		IdleTimeout:        0,
		ScalerType:         e.Scaling.Type,
		WorkersMin:         e.Workers.Min,
		WorkersMax:         e.Workers.Max,
		ExecutionTimeoutMs: e.Timeout,
		URLs:               invokeURLs(e.ID),
	}
	if e.Workers.IdleTimeout != nil {
		out.IdleTimeout = *e.Workers.IdleTimeout
	}
	switch {
	case e.Scaling.QueueDelay != nil:
		out.ScalerValue = int(math.Round(*e.Scaling.QueueDelay))
	case e.Scaling.RequestCount != nil:
		out.ScalerValue = *e.Scaling.RequestCount
	}
	flashboot := e.Flashboot != "" && !strings.EqualFold(e.Flashboot, "OFF")
	out.Flashboot = &flashboot
	if len(e.NetworkVolumes) > 0 {
		out.NetworkVolumeID = e.NetworkVolumes[0]
		for _, v := range e.NetworkVolumes {
			out.NetworkVolumeIDs = append(out.NetworkVolumeIDs, EndpointNetworkVolume{NetworkVolumeID: v})
		}
	}
	if e.Gpu != nil {
		out.GpuCount = e.Gpu.Count
		out.MinCudaVersion = e.Gpu.MinCudaVersion
		excluded := map[string]bool{}
		for _, t := range e.Gpu.ExcludedTypes {
			excluded[strings.ToUpper(t)] = true
		}
		for _, pool := range e.Gpu.Pools {
			for _, t := range poolTypes[pool] {
				if !excluded[strings.ToUpper(t)] {
					out.GpuTypeIDs = append(out.GpuTypeIDs, t)
				}
			}
		}
	}
	return out
}

// inlineTemplate is what --include-template can still show: v2 inlines the
// container config into the endpoint and keeps no template link, so there is
// no template id or name to report.
func (e *v2Endpoint) inlineTemplate() map[string]interface{} {
	t := map[string]interface{}{
		"imageName":         e.Image,
		"containerDiskInGb": e.Disk,
		"dockerArgs":        e.Args,
		"env":               e.Env,
		"ports":             strings.Join(e.Ports, ","),
		"isServerless":      true,
	}
	if e.Registry != nil {
		t["containerRegistryAuthId"] = *e.Registry
	}
	return t
}

// gpuPoolTypes maps each serverless pool to its gpu type ids, from the catalog.
func (c *Client) gpuPoolTypes() (map[string][]string, error) {
	pools, err := c.ListServerlessGpuPools()
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(pools))
	for _, p := range pools {
		out[p.ID] = p.GpuTypeIDs
	}
	return out, nil
}

func (c *Client) getV2Endpoint(endpointID string) (*v2Endpoint, error) {
	data, err := c.GetV2("/serverless/"+url.PathEscape(endpointID), nil)
	if err != nil {
		return nil, err
	}
	var e v2Endpoint
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &e, nil
}

func (c *Client) listV2Endpoints() ([]v2Endpoint, error) {
	var endpoints []v2Endpoint
	err := c.getV2AllPages("/serverless", nil, func(data []byte) error {
		var page struct {
			Endpoints []v2Endpoint `json:"endpoints"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
		endpoints = append(endpoints, page.Endpoints...)
		return nil
	})
	return endpoints, err
}

// decorateEndpoint applies the --include-template / --include-workers
// expansions. workers come from the v2 worker listing (the v1 expansion
// reported every worker of a warm endpoint as EXITED). a failed worker read is
// an error: without it the endpoint would print as one with no workers.
func (c *Client) decorateEndpoint(out *Endpoint, e *v2Endpoint, opts *EndpointListOptions, workers *V2Client) error {
	if opts == nil {
		return nil
	}
	if opts.IncludeTemplate {
		out.Template = e.inlineTemplate()
	}
	if opts.IncludeWorkers {
		list, err := workers.ListEndpointWorkersWithTimeout(e.ID)
		if err != nil {
			return fmt.Errorf("failed to read workers of endpoint %s: %w", e.ID, err)
		}
		out.Workers = make([]interface{}, 0, len(list.Workers))
		for _, w := range list.Workers {
			out.Workers = append(out.Workers, w)
		}
	}
	return nil
}

// workerClient returns the v2 client for the worker listing, sharing this
// client's v2 host and transport. nil when workers are not requested.
func (c *Client) workerClient(opts *EndpointListOptions) (*V2Client, error) {
	if opts == nil || !opts.IncludeWorkers {
		return nil, nil
	}
	v2, err := NewV2Client()
	if err != nil {
		return nil, err
	}
	v2.baseURL = c.v2URL()
	v2.httpClient = c.httpClient
	return v2, nil
}
