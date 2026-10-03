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
	// every pod), so the filters are applied here. the name filter is an exact,
	// case-insensitive match, as v1's was.
	pods := make([]Pod, 0, len(v2Pods))
	for i := range v2Pods {
		p := &v2Pods[i]
		if opts != nil {
			if opts.Name != "" && !strings.EqualFold(p.Name, opts.Name) {
				continue
			}
			// a pod reports gpu or cpu, never both
			if strings.EqualFold(opts.ComputeType, "GPU") && p.Gpu == nil ||
				strings.EqualFold(opts.ComputeType, "CPU") && p.Gpu != nil {
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

// v2PodUpdate is the rest v2 pod PATCH body.
type v2PodUpdate struct {
	Name   string            `json:"name,omitempty"`
	Image  string            `json:"image,omitempty"`
	Disk   int               `json:"disk,omitempty"`
	Ports  []string          `json:"ports,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	Mounts *v2PodMounts      `json:"mounts,omitempty"`
}

type v2NetworkMount struct {
	VolumeID string `json:"volumeId"`
	Path     string `json:"path"`
}

type v2PodMounts struct {
	Persistent *v2PersistentMount `json:"persistent,omitempty"`
	Network    []v2NetworkMount   `json:"network,omitempty"`
}

// UpdatePod updates an existing pod. v2 takes a mount whole (size and path) and
// keeps its kind, where v1 took volumeInGb or volumeMountPath alone, so a volume
// change is completed from the pod's current mount.
func (c *Client) UpdatePod(podID string, req *PodUpdateRequest) (*Pod, error) {
	body := &v2PodUpdate{
		Name:  req.Name,
		Image: req.ImageName,
		Disk:  req.ContainerDiskInGb,
		Ports: req.Ports,
		Env:   req.Env,
	}
	if req.VolumeInGb > 0 || req.VolumeMountPath != "" {
		current, err := c.GetPod(podID, false, false)
		if err != nil {
			return nil, err
		}
		path := req.VolumeMountPath
		if path == "" {
			path = current.VolumeMountPath
		}
		if current.NetworkVolumeID != "" {
			// a network volume's size belongs to the volume, not the pod; only
			// its mount path can change
			body.Mounts = &v2PodMounts{Network: []v2NetworkMount{{VolumeID: current.NetworkVolumeID, Path: path}}}
		} else {
			size := req.VolumeInGb
			if size == 0 {
				size = current.VolumeInGb
			}
			body.Mounts = &v2PodMounts{Persistent: &v2PersistentMount{Size: size, Path: path}}
		}
	}
	data, err := c.PatchV2("/pods/"+url.PathEscape(podID), body)
	if err != nil {
		return nil, err
	}
	return parseV2Pod(data)
}

// podAction triggers a v2 lifecycle action and returns the pod as it stands
// afterwards.
func (c *Client) podAction(podID, action string) (*Pod, error) {
	data, err := c.PostV2("/pods/"+url.PathEscape(podID)+"/action", map[string]string{"action": action})
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) > 0 {
		return parseV2Pod(data)
	}
	return c.GetPod(podID, false, false)
}

// StartPod starts a stopped pod
func (c *Client) StartPod(podID string) (*Pod, error) {
	return c.podAction(podID, "start")
}

// StopPod stops a running pod
func (c *Client) StopPod(podID string) (*Pod, error) {
	return c.podAction(podID, "stop")
}

// DeletePod deletes a pod
func (c *Client) DeletePod(podID string) error {
	_, err := c.DeleteV2("/pods/" + url.PathEscape(podID))
	return err
}

// RestartPod restarts a pod
func (c *Client) RestartPod(podID string) (*Pod, error) {
	return c.podAction(podID, "restart")
}
