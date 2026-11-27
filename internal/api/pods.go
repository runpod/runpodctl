package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	runpod "github.com/runpod/go-sdk/v2"
	"github.com/runpod/go-sdk/v2/lib/types"
)

// Pod represents a runpod pod
type Pod struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	DesiredStatus     string                 `json:"desiredStatus"`
	CreatedAt         interface{}            `json:"createdAt,omitempty"`
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
	v2Pods, err := c.listSDKPods()
	if err != nil {
		return nil, err
	}

	// v1 filtered server-side; v2 ignores unknown query parameters (returning
	// every pod), so the filters are applied here. the name filter is an exact,
	// case-insensitive match, as v1's was.
	pods := make([]Pod, 0, len(v2Pods))
	for _, p := range v2Pods {
		if opts != nil {
			if opts.Name != "" && !strings.EqualFold(p.Name, opts.Name) {
				continue
			}
			// a pod reports gpu or cpu, never both
			if strings.EqualFold(opts.ComputeType, "GPU") && p.Gpu == nil ||
				strings.EqualFold(opts.ComputeType, "CPU") && p.Gpu != nil {
				continue
			}
			if len(opts.GpuTypeIDs) > 0 && (p.Gpu == nil || !containsFold(opts.GpuTypeIDs, p.Gpu.Id)) {
				continue
			}
			if len(opts.DataCenterIDs) > 0 && !containsFold(opts.DataCenterIDs, deref(p.DataCenterId)) {
				continue
			}
		}
		pods = append(pods, podFromSDK(p))
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
	p, err := c.getSDKPod(podID)
	if err != nil {
		return nil, err
	}
	pod := podFromSDK(*p)

	if includeMachine {
		pod.Machine = map[string]interface{}{
			"dataCenterId": deref(p.DataCenterId),
			"secureCloud":  strings.EqualFold(string(p.Cloud), "SECURE"),
		}
	}
	if includeNetworkVolume && pod.NetworkVolumeID != "" {
		// v1 embedded the volume in the pod read; a failed lookup must not
		// print as a pod without one
		volume, err := c.GetNetworkVolume(pod.NetworkVolumeID)
		if err != nil {
			return nil, fmt.Errorf("failed to read network volume %s: %w", pod.NetworkVolumeID, err)
		}
		pod.NetworkVolume = volume
	}
	return &pod, nil
}

// UpdatePod updates an existing pod. v2 takes a mount whole (size and path) and
// keeps its kind, where v1 took volumeInGb or volumeMountPath alone, so a volume
// change is completed from the pod's current mount.
func (c *Client) UpdatePod(podID string, req *PodUpdateRequest) (*Pod, error) {
	if err := requireID(podID); err != nil {
		return nil, err
	}
	body := types.UpdatePodRequest{
		Name:  optional(req.Name),
		Image: optional(req.ImageName),
		Ports: nonEmpty(req.Ports),
	}
	if req.ContainerDiskInGb != 0 {
		body.Disk = &req.ContainerDiskInGb
	}
	if len(req.Env) > 0 {
		body.Env = &req.Env
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
			if req.VolumeInGb > 0 {
				return nil, fmt.Errorf("pod %s uses network volume %s, which a pod update cannot resize; use 'runpodctl network-volume update %s --size <gb>'", podID, current.NetworkVolumeID, current.NetworkVolumeID)
			}
			body.Mounts = &types.UpdatePodMounts{Network: &[]types.NetworkMountPatch{{VolumeId: current.NetworkVolumeID, Path: &path}}}
		} else {
			size := req.VolumeInGb
			if size == 0 {
				size = current.VolumeInGb
			}
			body.Mounts = &types.UpdatePodMounts{Persistent: &types.PersistentMountPatch{Size: &size, Path: &path}}
		}
	}
	p, err := sdkCall[types.Pod](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Pods.UpdatePod(ctx, podID, body)
	})
	if err != nil {
		return nil, err
	}
	pod := podFromSDK(*p)
	return &pod, nil
}

// podAction triggers a v2 lifecycle action and returns the pod as it stands
// afterwards.
func (c *Client) podAction(podID string, action types.PodAction) (*Pod, error) {
	if err := requireID(podID); err != nil {
		return nil, err
	}
	data, err := c.sdkSend(func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Pods.PodAction(ctx, podID, types.PodActionRequest{Action: action})
	})
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) > 0 {
		return parseSDKPod(data)
	}
	return c.GetPod(podID, false, false)
}

// StartPod starts a stopped pod
func (c *Client) StartPod(podID string) (*Pod, error) {
	return c.podAction(podID, types.PodActionStart)
}

// StopPod stops a running pod
func (c *Client) StopPod(podID string) (*Pod, error) {
	return c.podAction(podID, types.PodActionStop)
}

// DeletePod deletes a pod
func (c *Client) DeletePod(podID string) error {
	if err := requireID(podID); err != nil {
		return err
	}
	_, err := c.sdkSend(func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Pods.DeletePod(ctx, podID)
	})
	return err
}

// RestartPod restarts a pod
func (c *Client) RestartPod(podID string) (*Pod, error) {
	return c.podAction(podID, types.PodActionRestart)
}
