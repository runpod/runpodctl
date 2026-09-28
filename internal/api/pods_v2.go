package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type podV2ListResponse struct {
	Pods       []podV2         `json:"pods"`
	Pagination podV2Pagination `json:"pagination"`
}

type podV2Pagination struct {
	NextCursor  string `json:"nextCursor"`
	HasNextPage bool   `json:"hasNextPage"`
}

type podV2 struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Image            string                 `json:"image"`
	Args             string                 `json:"args"`
	Cmd              []string               `json:"cmd"`
	Entrypoint       []string               `json:"entrypoint"`
	Disk             int                    `json:"disk"`
	Ports            []string               `json:"ports"`
	Env              map[string]string      `json:"env"`
	Status           string                 `json:"status"`
	Mounts           podV2Mounts            `json:"mounts"`
	GPU              *podV2GPU              `json:"gpu"`
	CPU              *podV2CPU              `json:"cpu"`
	Cloud            string                 `json:"cloud"`
	DataCenterID     string                 `json:"dataCenterId"`
	CUDA             string                 `json:"cudaVersion"`
	Template         string                 `json:"template"`
	Cost             float64                `json:"cost"`
	Runtime          map[string]interface{} `json:"runtime"`
	CreatedAt        interface{}            `json:"createdAt"`
	StartedAt        interface{}            `json:"startedAt"`
	GlobalNetworking map[string]interface{} `json:"globalNetworking"`
}

type podV2GPU struct {
	ID        string `json:"id"`
	Count     int    `json:"count"`
	VCPUCount int    `json:"vcpuCount"`
	Memory    int    `json:"memory"`
}

type podV2CPU struct {
	ID        string `json:"id"`
	VCPUCount int    `json:"vcpuCount"`
	Memory    int    `json:"memory"`
}

type podV2Mounts struct {
	Persistent *PodV2PersistentMount `json:"persistent"`
	Network    []PodV2NetworkMount   `json:"network"`
}

type PodV2PersistentMount struct {
	Size int    `json:"size"`
	Path string `json:"path"`
}

type PodV2NetworkMount struct {
	VolumeID string `json:"volumeId"`
	Path     string `json:"path"`
}

type PodV2CreateRequest struct {
	Name             string             `json:"name"`
	Image            string             `json:"image,omitempty"`
	TemplateID       string             `json:"templateId,omitempty"`
	Args             string             `json:"args,omitempty"`
	Disk             int                `json:"disk"`
	Ports            []string           `json:"ports"`
	Env              map[string]string  `json:"env"`
	Cloud            string             `json:"cloud,omitempty"`
	DataCenterIDs    []string           `json:"dataCenterIds,omitempty"`
	GlobalNetworking bool               `json:"globalNetworking,omitempty"`
	StartSSH         bool               `json:"startSsh,omitempty"`
	Registry         string             `json:"registry,omitempty"`
	GPU              *PodV2GPURequest   `json:"gpu,omitempty"`
	CPU              *PodV2CPURequest   `json:"cpu,omitempty"`
	Mounts           *PodV2MountRequest `json:"mounts,omitempty"`
}

type PodV2GPURequest struct {
	ID                 string `json:"id"`
	Count              int    `json:"count"`
	MinCUDA            string `json:"minCudaVersion,omitempty"`
	MinRAMPerGPU       int    `json:"minRamPerGpu,omitempty"`
	MinVCPUCountPerGPU int    `json:"minVcpuCountPerGpu,omitempty"`
}

type PodV2CPURequest struct {
	ID        string `json:"id"`
	VCPUCount int    `json:"vcpuCount"`
}

type PodV2MountRequest struct {
	Persistent *PodV2PersistentMount `json:"persistent,omitempty"`
	Network    []PodV2NetworkMount   `json:"network,omitempty"`
}

type PodV2UpdateRequest struct {
	Name            string            `json:"-"`
	Image           string            `json:"-"`
	Disk            int               `json:"-"`
	VolumeInGb      int               `json:"-"`
	VolumeMountPath string            `json:"-"`
	Ports           []string          `json:"-"`
	Env             map[string]string `json:"-"`
}

// ListPodsV2 returns every pod, following the v2 cursor until the last page.
func (c *V2Client) ListPodsV2(opts *PodListOptions) ([]Pod, error) {
	var pods []Pod
	params := url.Values{"limit": []string{"1000"}}
	for {
		path := "/pods?" + params.Encode()
		data, err := c.requestWithTimeout(http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		var page podV2ListResponse
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		for _, raw := range page.Pods {
			if podV2Matches(raw, opts) {
				pods = append(pods, raw.toPod())
			}
		}
		if !page.Pagination.HasNextPage {
			return pods, nil
		}
		if page.Pagination.NextCursor == "" {
			return nil, fmt.Errorf("failed to parse response: v2 pod list has another page but no cursor")
		}
		params.Set("cursor", page.Pagination.NextCursor)
	}
}

func podV2Matches(p podV2, opts *PodListOptions) bool {
	if opts == nil {
		return true
	}
	if opts.ComputeType != "" {
		isGPU := p.GPU != nil
		if (strings.EqualFold(opts.ComputeType, "GPU") && !isGPU) || (strings.EqualFold(opts.ComputeType, "CPU") && isGPU) {
			return false
		}
	}
	if opts.Name != "" && !strings.Contains(strings.ToLower(p.Name), strings.ToLower(opts.Name)) {
		return false
	}
	if len(opts.GpuTypeIDs) > 0 {
		if p.GPU == nil || !containsFold(opts.GpuTypeIDs, p.GPU.ID) {
			return false
		}
	}
	if len(opts.DataCenterIDs) > 0 && !containsFold(opts.DataCenterIDs, p.DataCenterID) {
		return false
	}
	return true
}

func containsFold(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}

func (c *V2Client) GetPodV2(podID string, includeMachine, includeNetworkVolume bool) (*Pod, error) {
	raw, err := c.getPodV2(podID)
	if err != nil {
		return nil, err
	}
	pod := raw.toPod()
	if includeMachine || (includeNetworkVolume && len(raw.Mounts.Network) > 0) {
		pod.Machine = map[string]interface{}{
			"cloud":        raw.Cloud,
			"dataCenterId": raw.DataCenterID,
			"cudaVersion":  raw.CUDA,
		}
		if raw.GPU != nil {
			pod.Machine["gpuDisplayName"] = raw.GPU.ID
		}
		if raw.CPU != nil {
			pod.Machine["cpuId"] = raw.CPU.ID
		}
		if raw.GlobalNetworking != nil {
			pod.Machine["globalNetworking"] = raw.GlobalNetworking
		}
	}
	if includeNetworkVolume && len(raw.Mounts.Network) > 0 {
		volume, err := c.GetNetworkVolumeV2(raw.Mounts.Network[0].VolumeID)
		if err != nil {
			return nil, fmt.Errorf("failed to get network volume: %w", err)
		}
		pod.NetworkVolume = volume
	}
	return &pod, nil
}

func (c *V2Client) GetNetworkVolumeV2(volumeID string) (*NetworkVolume, error) {
	data, err := c.requestWithTimeout(http.MethodGet, "/network-volumes/"+url.PathEscape(volumeID), nil)
	if err != nil {
		return nil, err
	}
	var volume NetworkVolume
	if err := json.Unmarshal(data, &volume); err != nil {
		return nil, fmt.Errorf("failed to parse network volume response: %w", err)
	}
	return &volume, nil
}

func (c *V2Client) getPodV2(podID string) (*podV2, error) {
	data, err := c.requestWithTimeout(http.MethodGet, "/pods/"+url.PathEscape(podID), nil)
	if err != nil {
		return nil, err
	}
	var raw podV2
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &raw, nil
}

func (c *V2Client) CreatePodV2(req *PodV2CreateRequest) (*Pod, error) {
	data, err := c.requestWithTimeout(http.MethodPost, "/pods", req)
	if err != nil {
		return nil, err
	}
	var raw podV2
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	pod := raw.toPod()
	return &pod, nil
}

func (c *V2Client) UpdatePodV2(podID string, req *PodV2UpdateRequest) (*Pod, error) {
	body := map[string]interface{}{}
	if req.Name != "" {
		body["name"] = req.Name
	}
	if req.Image != "" {
		body["image"] = req.Image
	}
	if req.Disk > 0 {
		body["disk"] = req.Disk
	}
	if req.Ports != nil {
		body["ports"] = req.Ports
	}
	if req.Env != nil {
		body["env"] = req.Env
	}
	if req.VolumeInGb > 0 || req.VolumeMountPath != "" {
		raw, err := c.getPodV2(podID)
		if err != nil {
			return nil, err
		}
		mounts := &PodV2MountRequest{}
		switch {
		case raw.Mounts.Persistent != nil:
			mount := *raw.Mounts.Persistent
			if req.VolumeInGb > 0 {
				mount.Size = req.VolumeInGb
			}
			if req.VolumeMountPath != "" {
				mount.Path = req.VolumeMountPath
			}
			mounts.Persistent = &mount
		case len(raw.Mounts.Network) > 0:
			if req.VolumeInGb > 0 {
				return nil, fmt.Errorf("network volume size cannot be changed with pod update")
			}
			mount := raw.Mounts.Network[0]
			if req.VolumeMountPath != "" {
				mount.Path = req.VolumeMountPath
			}
			mounts.Network = []PodV2NetworkMount{mount}
		default:
			return nil, fmt.Errorf("pod has no mount to update")
		}
		body["mounts"] = mounts
	}
	data, err := c.requestWithTimeout(http.MethodPatch, "/pods/"+url.PathEscape(podID), body)
	if err != nil {
		return nil, err
	}
	var raw podV2
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	pod := raw.toPod()
	return &pod, nil
}

func (c *V2Client) podActionV2(podID, action string) (*Pod, error) {
	data, err := c.requestWithTimeout(http.MethodPost, "/pods/"+url.PathEscape(podID)+"/action", map[string]string{"action": action})
	if err != nil {
		return nil, err
	}
	var raw podV2
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	pod := raw.toPod()
	return &pod, nil
}

func (c *V2Client) StartPodV2(podID string) (*Pod, error) {
	return c.podActionV2(podID, "start")
}

func (c *V2Client) StopPodV2(podID string) (*Pod, error) {
	return c.podActionV2(podID, "stop")
}

func (c *V2Client) RestartPodV2(podID string) (*Pod, error) {
	return c.podActionV2(podID, "restart")
}

func (c *V2Client) DeletePodV2(podID string) error {
	_, err := c.requestWithTimeout(http.MethodDelete, "/pods/"+url.PathEscape(podID), nil)
	return err
}

func (p podV2) toPod() Pod {
	pod := Pod{
		ID:                p.ID,
		Name:              p.Name,
		DesiredStatus:     p.Status,
		CreatedAt:         p.CreatedAt,
		ImageName:         p.Image,
		ContainerDiskInGb: p.Disk,
		Ports:             p.Ports,
		DockerStartCmd:    p.Cmd,
		DockerEntrypoint:  p.Entrypoint,
		CostPerHr:         p.Cost,
		Runtime:           p.Runtime,
		Env:               p.Env,
	}
	if p.GPU != nil {
		pod.GpuTypeID = p.GPU.ID
		pod.GpuCount = p.GPU.Count
		pod.VcpuCount = p.GPU.VCPUCount
		pod.MemoryInGb = p.GPU.Memory
	}
	if p.CPU != nil {
		pod.VcpuCount = p.CPU.VCPUCount
		pod.MemoryInGb = p.CPU.Memory
	}
	if p.Mounts.Persistent != nil {
		pod.VolumeInGb = p.Mounts.Persistent.Size
		pod.VolumeMountPath = p.Mounts.Persistent.Path
	} else if len(p.Mounts.Network) > 0 {
		pod.NetworkVolumeID = p.Mounts.Network[0].VolumeID
		pod.VolumeMountPath = p.Mounts.Network[0].Path
	}
	if uptime, ok := p.Runtime["uptime"].(float64); ok {
		pod.UptimeSeconds = strconv.FormatInt(int64(uptime), 10)
	}
	return pod
}
