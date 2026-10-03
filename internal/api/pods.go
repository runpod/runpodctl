package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
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
	DockerStartCmd    []string               `json:"dockerStartCmd,omitempty"`
	DockerEntrypoint  []string               `json:"dockerEntrypoint,omitempty"`
	CostPerHr         float64                `json:"costPerHr,omitempty"`
	NetworkVolumeID   string                 `json:"networkVolumeId,omitempty"`
	NetworkVolume     *NetworkVolume         `json:"networkVolume,omitempty"`
	Machine           map[string]interface{} `json:"machine,omitempty"`
	Runtime           map[string]interface{} `json:"runtime,omitempty"`
	Env               map[string]string      `json:"env,omitempty"`

	// legacy is the runtime/ssh view of the same v2 read; see Legacy.
	legacy *LegacyPod
}

// Legacy returns the runtime/ssh view of the pod (runtime telemetry and port
// mappings), from the same read, or nil when the pod did not come from rest v2.
func (p *Pod) Legacy() *LegacyPod {
	return p.legacy
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
	DockerStartCmd    []string          `json:"dockerStartCmd,omitempty"`
	DockerEntrypoint  []string          `json:"dockerEntrypoint,omitempty"`
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
	v2Pods, err := c.listV2Pods()
	if err != nil {
		return nil, err
	}

	// v1 filtered server-side; v2 ignores unknown query parameters (returning
	// every pod), so the filters v1 honoured are applied here. v1's name filter
	// is an exact, case-insensitive match. its computeType filter had no effect
	// (a cpu pod came back for computeType=GPU), so it is not applied either.
	pods := make([]Pod, 0, len(v2Pods))
	for i := range v2Pods {
		p := &v2Pods[i]
		if opts != nil {
			if opts.Name != "" && !strings.EqualFold(p.Name, opts.Name) {
				continue
			}
			if len(opts.GpuTypeIDs) > 0 && (p.Gpu == nil || !containsFold(opts.GpuTypeIDs, p.Gpu.ID)) {
				continue
			}
			if len(opts.DataCenterIDs) > 0 && !containsFold(opts.DataCenterIDs, p.DataCenterID) {
				continue
			}
		}
		pods = append(pods, p.toPod())
	}
	return pods, nil
}

func containsFold(values []string, v string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, v) {
			return true
		}
	}
	return false
}

// PodListOptions are options for listing pods
type PodListOptions struct {
	ComputeType   string
	GpuTypeIDs    []string
	DataCenterIDs []string
	Name          string
}

// GetPod returns a single pod by ID. v2 has no machine details: includeMachine
// reports only the data center and whether it is secure cloud. includeNetworkVolume
// reads the attached volume separately, as v2 reports only its id.
func (c *Client) GetPod(podID string, includeMachine, includeNetworkVolume bool) (*Pod, error) {
	data, err := c.GetV2("/pods/"+url.PathEscape(podID), nil)
	if err != nil {
		return nil, err
	}

	var p v2Pod
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	pod := p.toPod()

	if includeMachine {
		pod.Machine = map[string]interface{}{
			"dataCenterId": p.DataCenterID,
			"secureCloud":  strings.EqualFold(p.Cloud, "SECURE"),
		}
	}
	if includeNetworkVolume && pod.NetworkVolumeID != "" {
		if volume, err := c.GetNetworkVolume(pod.NetworkVolumeID); err == nil {
			pod.NetworkVolume = volume
		}
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
