package api

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

// v2Pod is a pod as rest v2 reports it. toPod maps it onto the field names the
// cli has always printed; toLegacyPod builds the runtime/ssh view that
// internal/sshconnect and internal/podstate consume.
//
// v2 has no lastStatusChange and no machine details, so those are not reported
// (`--include-machine` carries only dataCenterId and secureCloud).
type v2Pod struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	Image     string `json:"image"`
	Gpu       *struct {
		ID        string  `json:"id"`
		Count     int     `json:"count"`
		VcpuCount float64 `json:"vcpuCount"`
		Memory    float64 `json:"memory"`
	} `json:"gpu"`
	Cpu *struct {
		ID        string  `json:"id"`
		VcpuCount float64 `json:"vcpuCount"`
		Memory    float64 `json:"memory"`
	} `json:"cpu"`
	Disk   int `json:"disk"`
	Mounts struct {
		Persistent *v2PersistentMount `json:"persistent"`
		Network    []struct {
			VolumeID string `json:"volumeId"`
			Path     string `json:"path"`
		} `json:"network"`
	} `json:"mounts"`
	Ports        []string          `json:"ports"`
	Cmd          []string          `json:"cmd"`
	Entrypoint   []string          `json:"entrypoint"`
	Cost         float64           `json:"cost"`
	Env          map[string]string `json:"env"`
	DataCenterID string            `json:"dataCenterId"`
	Cloud        string            `json:"cloud"`
	Runtime      *struct {
		Uptime *int `json:"uptime"`
		Ports  []struct {
			IP      *string `json:"ip"`
			Private int     `json:"private"`
			Public  *int    `json:"public"`
			Type    string  `json:"type"`
		} `json:"ports"`
	} `json:"runtime"`
}

// legacyDesiredStatus maps v2's observed status onto the requested-state value
// the cli has always printed as desiredStatus: a pod being provisioned or
// started was asked to run.
func legacyDesiredStatus(status string) string {
	switch strings.ToUpper(status) {
	// ERROR is v2's "unrecoverable" state and is passed through, so a wait
	// ends instead of treating a dead pod as one still booting
	case "PROVISIONING", "STARTING", "RUNNING":
		return "RUNNING"
	default:
		return strings.ToUpper(status)
	}
}

// legacyTimestamp renders a v2 RFC 3339 time the way v1 did (Go's time.String
// layout, e.g. "2026-10-02 05:28:38.366 +0000 UTC"), which `pod get` has always
// printed. An unparseable value is passed through unchanged.
func legacyTimestamp(v string) interface{} {
	if v == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return v
	}
	return t.UTC().String()
}

func (p *v2Pod) memoryAndVcpu() (memory, vcpu int) {
	switch {
	case p.Gpu != nil:
		return int(p.Gpu.Memory), int(p.Gpu.VcpuCount)
	case p.Cpu != nil:
		return int(p.Cpu.Memory), int(p.Cpu.VcpuCount)
	}
	return 0, 0
}

func (p *v2Pod) volume() (sizeGb int, mountPath, networkVolumeID string) {
	if m := p.Mounts.Persistent; m != nil {
		sizeGb, mountPath = m.Size, m.Path
	}
	if len(p.Mounts.Network) > 0 {
		networkVolumeID = p.Mounts.Network[0].VolumeID
		if mountPath == "" {
			mountPath = p.Mounts.Network[0].Path
		}
	}
	return sizeGb, mountPath, networkVolumeID
}

func (p *v2Pod) toPod() Pod {
	memory, vcpu := p.memoryAndVcpu()
	volumeInGb, mountPath, networkVolumeID := p.volume()
	pod := Pod{
		ID:                p.ID,
		Name:              p.Name,
		DesiredStatus:     legacyDesiredStatus(p.Status),
		CreatedAt:         legacyTimestamp(p.CreatedAt),
		ImageName:         p.Image,
		VolumeInGb:        volumeInGb,
		ContainerDiskInGb: p.Disk,
		MemoryInGb:        memory,
		VcpuCount:         vcpu,
		VolumeMountPath:   mountPath,
		Ports:             p.Ports,
		DockerStartCmd:    p.Cmd,
		DockerEntrypoint:  p.Entrypoint,
		CostPerHr:         p.Cost,
		NetworkVolumeID:   networkVolumeID,
		legacy:            p.toLegacyPod(),
	}
	if p.Gpu != nil {
		pod.GpuTypeID = p.Gpu.ID
		pod.GpuCount = p.Gpu.Count
	}
	if len(p.Env) > 0 {
		pod.Env = p.Env
	}
	return pod
}

// isPublicIP reports whether ip is reachable from the internet. graphql flagged
// this per port (isIpPublic); v2 does not, and its http ports report an address
// on the runpod proxy's carrier-grade nat range.
func isPublicIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() {
		return false
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return !cgnat.Contains(parsed)
}

func (p *v2Pod) toLegacyPod() *LegacyPod {
	memory, vcpu := p.memoryAndVcpu()
	volumeInGb, mountPath, _ := p.volume()
	legacy := &LegacyPod{
		ID:                p.ID,
		ContainerDiskInGb: p.Disk,
		CostPerHr:         float32(p.Cost),
		DesiredStatus:     legacyDesiredStatus(p.Status),
		ImageName:         p.Image,
		MemoryInGb:        memory,
		Name:              p.Name,
		Ports:             strings.Join(p.Ports, ","),
		VcpuCount:         vcpu,
		VolumeInGb:        volumeInGb,
		VolumeMountPath:   mountPath,
	}
	if p.Gpu != nil {
		legacy.GpuCount = p.Gpu.Count
	}
	for k, v := range p.Env {
		legacy.Env = append(legacy.Env, k+"="+v)
	}
	if p.Runtime != nil {
		runtime := &LegacyRuntime{UptimeInSeconds: p.Runtime.Uptime, Ports: []*LegacyPort{}}
		for _, port := range p.Runtime.Ports {
			lp := &LegacyPort{PrivatePort: port.Private, PortType: port.Type}
			if port.IP != nil {
				lp.Ip = *port.IP
				lp.IsIpPublic = port.Type == "tcp" && isPublicIP(*port.IP)
			}
			if port.Public != nil {
				lp.PublicPort = *port.Public
			}
			runtime.Ports = append(runtime.Ports, lp)
		}
		legacy.Runtime = runtime
	}
	return legacy
}

func parseV2Pod(data []byte) (*Pod, error) {
	var p v2Pod
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	pod := p.toPod()
	return &pod, nil
}

// listV2Pods reads every pod, following v2's pagination.
func (c *Client) listV2Pods() ([]v2Pod, error) {
	var pods []v2Pod
	err := c.getV2AllPages("/pods", nil, func(data []byte) error {
		var page struct {
			Pods []v2Pod `json:"pods"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
		pods = append(pods, page.Pods...)
		return nil
	})
	return pods, err
}

// GetPods returns every pod in the runtime/ssh view that internal/sshconnect,
// internal/podstate and the --wait poll consume. It replaces the graphql
// myPods read: v2 reports runtime telemetry and ports directly.
func (c *Client) GetPods() ([]*LegacyPod, error) {
	pods, err := c.listV2Pods()
	if err != nil {
		return nil, err
	}
	out := make([]*LegacyPod, 0, len(pods))
	for i := range pods {
		out = append(out, pods[i].toLegacyPod())
	}
	return out, nil
}

// GetLegacyPods returns every pod as the legacy `get pod` command prints it: the runtime view plus the graphql machine block and
// podType. v2 reports neither, so the gpu display name comes from the catalog
// and the location from the data center id (legacyLocation). v2 cannot tell a
// spot pod from an on-demand one, so podType is always RESERVED.
func (c *Client) GetLegacyPods() ([]*LegacyPod, error) {
	pods, err := c.listV2Pods()
	if err != nil {
		return nil, err
	}
	var gpuNames map[string]string
	out := make([]*LegacyPod, 0, len(pods))
	for i := range pods {
		p := &pods[i]
		legacy := p.toLegacyPod()
		legacy.PodType = "RESERVED"
		machine := &LegacyMachine{GpuDisplayName: "unknown", Location: legacyLocation(p.DataCenterID)}
		if p.Gpu != nil && p.Gpu.ID != "" {
			if gpuNames == nil {
				gpuNames = c.catalogGpuNames()
			}
			machine.GpuDisplayName = p.Gpu.ID
			if name := gpuNames[p.Gpu.ID]; name != "" {
				machine.GpuDisplayName = name
			}
		}
		legacy.Machine = machine
		out = append(out, legacy)
	}
	return out, nil
}

// catalogGpuNames maps gpu type ids to the display names graphql reported as
// machine.gpuDisplayName (the catalog's name is the same string). best-effort:
// on failure the caller falls back to the gpu type id.
func (c *Client) catalogGpuNames() map[string]string {
	names := map[string]string{}
	gpus, err := c.listCatalogGpus(nil)
	if err != nil {
		return names
	}
	for _, gpu := range gpus {
		names[gpu.ID] = gpu.Name
	}
	return names
}

// legacyLocation approximates graphql's machine.location, a country code, from
// a data center id: "US-NC-2" -> "US", "EU-RO-1" -> "RO". ids that lead with a
// region rather than a country take the second segment.
func legacyLocation(dataCenterID string) string {
	parts := strings.Split(dataCenterID, "-")
	if len(parts) < 2 {
		return dataCenterID
	}
	switch parts[0] {
	case "EU", "EUR", "AP", "OC", "SEA", "SA", "AF", "ME":
		return parts[1]
	}
	return parts[0]
}
