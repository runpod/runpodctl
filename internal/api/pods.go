package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Pod represents a runpod pod
type Pod struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	DesiredStatus     string                 `json:"desiredStatus"`
	CreatedAt         interface{}            `json:"createdAt,omitempty"`
	LastStatusChange  interface{}            `json:"lastStatusChange,omitempty"`
	UptimeSeconds     interface{}            `json:"uptimeSeconds,omitempty"`
	ImageName         string                 `json:"imageName"`
	GpuTypeID         string                 `json:"gpuTypeId,omitempty"`
	GpuCount          int                    `json:"gpuCount"`
	VolumeInGb        int                    `json:"volumeInGb"`
	ContainerDiskInGb int                    `json:"containerDiskInGb"`
	MemoryInGb        int                    `json:"memoryInGb,omitempty"`
	VcpuCount         int                    `json:"vcpuCount,omitempty"`
	VolumeMountPath   string                 `json:"volumeMountPath,omitempty"`
	Ports             []string               `json:"ports,omitempty"`
	CostPerHr         float64                `json:"costPerHr,omitempty"`
	Machine           map[string]interface{} `json:"machine,omitempty"`
	Runtime           map[string]interface{} `json:"runtime,omitempty"`
	Env               map[string]string      `json:"env,omitempty"`
}

// PodListResponse is the response from listing pods
type PodListResponse struct {
	Pods []Pod `json:"pods"`
}

// PodCreateRequest is the request to create a pod
type PodCreateRequest struct {
	Name              string            `json:"name,omitempty"`
	ImageName         string            `json:"imageName,omitempty"`
	TemplateID        string            `json:"templateId,omitempty"`
	ComputeType       string            `json:"computeType,omitempty"`
	GlobalNetworking  bool              `json:"globalNetworking,omitempty"`
	SupportPublicIp   bool              `json:"supportPublicIp,omitempty"`
	GpuTypeIDs        []string          `json:"gpuTypeIds,omitempty"`
	GpuCount          int               `json:"gpuCount,omitempty"`
	VolumeInGb        int               `json:"volumeInGb,omitempty"`
	ContainerDiskInGb int               `json:"containerDiskInGb,omitempty"`
	VolumeMountPath   string            `json:"volumeMountPath,omitempty"`
	Ports             []string          `json:"ports,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	CloudType         string            `json:"cloudType,omitempty"`
	DataCenterIDs     []string          `json:"dataCenterIds,omitempty"`
	NetworkVolumeID   string            `json:"networkVolumeId,omitempty"`
	MinCudaVersion    string            `json:"minCudaVersion,omitempty"`
	DockerArgs        string            `json:"dockerArgs,omitempty"`
}

// PodUpdateRequest is the request to update a pod
type PodUpdateRequest struct {
	Name              string            `json:"name,omitempty"`
	ImageName         string            `json:"imageName,omitempty"`
	ContainerDiskInGb int               `json:"containerDiskInGb,omitempty"`
	VolumeInGb        int               `json:"volumeInGb,omitempty"`
	VolumeMountPath   string            `json:"volumeMountPath,omitempty"`
	Ports             []string          `json:"ports,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
}

// ListPods returns all pods
func (c *Client) ListPods(opts *PodListOptions) ([]Pod, error) {
	params := url.Values{}
	if opts != nil {
		if opts.ComputeType != "" {
			params.Set("computeType", opts.ComputeType)
		}
		if opts.Name != "" {
			params.Set("name", opts.Name)
		}
		for _, gpuType := range opts.GpuTypeIDs {
			params.Add("gpuTypeId", gpuType)
		}
		for _, dc := range opts.DataCenterIDs {
			params.Add("dataCenterId", dc)
		}
	}

	data, err := c.Get("/pods", params)
	if err != nil {
		return nil, err
	}

	var pods []Pod
	if err := json.Unmarshal(data, &pods); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return pods, nil
}

// PodListOptions are options for listing pods
type PodListOptions struct {
	ComputeType   string
	GpuTypeIDs    []string
	DataCenterIDs []string
	Name          string
}

// GetPod returns a single pod by ID
func (c *Client) GetPod(podID string, includeMachine, includeNetworkVolume bool) (*Pod, error) {
	params := url.Values{}
	if includeMachine {
		params.Set("includeMachine", "true")
	}
	if includeNetworkVolume {
		params.Set("includeNetworkVolume", "true")
	}

	data, err := c.Get("/pods/"+podID, params)
	if err != nil {
		return nil, err
	}

	var pod Pod
	if err := json.Unmarshal(data, &pod); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &pod, nil
}

// CreatePod creates a new pod
func (c *Client) CreatePod(req *PodCreateRequest) (*Pod, error) {
	data, err := c.Post("/pods", req)
	if err != nil {
		return nil, err
	}

	var pod Pod
	if err := json.Unmarshal(data, &pod); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &pod, nil
}

// UpdatePod updates an existing pod
func (c *Client) UpdatePod(podID string, req *PodUpdateRequest) (*Pod, error) {
	data, err := c.Patch("/pods/"+podID, req)
	if err != nil {
		return nil, err
	}

	var pod Pod
	if err := json.Unmarshal(data, &pod); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &pod, nil
}

// StartPod starts a stopped pod
func (c *Client) StartPod(podID string) (*Pod, error) {
	data, err := c.Post("/pods/"+podID+"/start", nil)
	if err != nil {
		return nil, err
	}

	var pod Pod
	if err := json.Unmarshal(data, &pod); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &pod, nil
}

// StopPod stops a running pod
func (c *Client) StopPod(podID string) (*Pod, error) {
	data, err := c.Post("/pods/"+podID+"/stop", nil)
	if err != nil {
		return nil, err
	}

	var pod Pod
	if err := json.Unmarshal(data, &pod); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &pod, nil
}

// DeletePod deletes a pod
func (c *Client) DeletePod(podID string) error {
	_, err := c.Delete("/pods/" + podID)
	return err
}

// ResetPod resets a pod
func (c *Client) ResetPod(podID string) (*Pod, error) {
	data, err := c.Post("/pods/"+podID+"/reset", nil)
	if err != nil {
		return nil, err
	}

	var pod Pod
	if err := json.Unmarshal(data, &pod); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &pod, nil
}

// RestartPod restarts a pod
func (c *Client) RestartPod(podID string) (*Pod, error) {
	data, err := c.Post("/pods/"+podID+"/restart", nil)
	if err != nil {
		return nil, err
	}

	var pod Pod
	if err := json.Unmarshal(data, &pod); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &pod, nil
}
