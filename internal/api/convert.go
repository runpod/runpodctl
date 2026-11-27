package api

import (
	"strconv"
	"strings"

	"github.com/runpod/go-sdk/v2/lib/types"
)

// Converters from the generated SDK's types to the structs the cli prints. The
// cli keeps its own structs so that its JSON output does not change shape when
// the API or the SDK does.

func networkVolumeFromSDK(v types.NetworkVolume) NetworkVolume {
	return NetworkVolume{ID: v.Id, Name: v.Name, Size: v.Size, DataCenterID: v.DataCenter}
}

// float32To64 widens an SDK float32 (the spec types prices as float) without
// the binary noise a plain conversion adds: 0.69 stays 0.69, not
// 0.6899999976158142.
func float32To64(f float32) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(float64(f), 'g', -1, 32), 64)
	return v
}

// gpuTypeFromSDK converts a catalog gpu read with pod availability. The overall
// status is the better of v2's top-level availability and the best data center:
// the top level also counts community stock that has no data center entry, but
// it can read NONE while a data center reads LOW (observed 2026-10-03 for the
// RTX A4000 without a cloud filter).
//
// BACKWARD_COMPATIBILITY: prints the field names (displayName, memoryInGb,
// securePrice, ...) and the High/Medium/Low stock spelling the cli has always
// used. Next major: print the API's names and levels.
func gpuTypeFromSDK(gpu types.GpuType) GpuTypeWithAvailability {
	var stockStatus string
	if gpu.Availability != nil {
		stockStatus = legacyStockStatus(string(*gpu.Availability))
	}
	var perDC []GpuDataCenterAvailability
	if gpu.DataCenters != nil {
		for _, dc := range *gpu.DataCenters {
			stock := legacyStockStatus(string(dc.Availability))
			if betterStock(stock, stockStatus) {
				stockStatus = stock
			}
			if stock == "" {
				stock = "none"
			}
			perDC = append(perDC, GpuDataCenterAvailability{DataCenterID: dc.Id, StockStatus: stock})
		}
	}
	return GpuTypeWithAvailability{
		GpuType: GpuType{
			ID:             gpu.Id,
			DisplayName:    gpu.Name,
			MemoryInGb:     gpu.Memory,
			SecureCloud:    gpu.Secure,
			CommunityCloud: gpu.Community,
			SecurePrice:    float32To64(gpu.Price.Secure),
			CommunityPrice: float32To64(gpu.Price.Community),
		},
		StockStatus:            stockStatus,
		Available:              hasStock(stockStatus),
		DataCenterAvailability: perDC,
	}
}

// dataCenterFromSDK converts a data center read with gpu availability. v2
// reports a region (e.g. NORTH_AMERICA) where graphql had a country-level
// location; it is carried in Location.
//
// BACKWARD_COMPATIBILITY: the region is printed as location, and stock with
// the High/Medium/Low spelling. Next major: print the API's region and levels.
func dataCenterFromSDK(dc types.DataCenter) DataCenter {
	out := DataCenter{ID: dc.Id, Name: dc.Name, Location: string(dc.Region)}
	if dc.GpuAvailability != nil {
		for _, g := range *dc.GpuAvailability {
			out.GpuAvailability = append(out.GpuAvailability, GpuAvailabilityInDataCenter{
				GpuTypeID:   g.Id,
				DisplayName: g.Name,
				StockStatus: legacyStockStatus(string(g.Availability)),
			})
		}
	}
	return out
}

// registryAuthFromSDK converts a registry auth.
func registryAuthFromSDK(r types.Registry) ContainerRegistryAuth {
	return ContainerRegistryAuth{ID: r.Id, Name: r.Name}
}

// templateFromSDK converts a rest v2 template into the field names the cli has
// always printed. readme, portsConfig and isRunpod have no v2 field and are
// filled in from graphql where the cli shows them.
//
// BACKWARD_COMPATIBILITY: prints the field names the cli has always used
// (imageName, dockerStartCmd, containerDiskInGb, ...), not the API's. Next
// major: print the API's names.
func templateFromSDK(t types.Template) Template {
	out := Template{
		ID:                t.Id,
		Name:              t.Name,
		ImageName:         t.Image,
		IsServerless:      t.Serverless,
		IsPublic:          t.Public,
		Category:          string(t.Category),
		Ports:             t.Ports,
		ContainerDiskInGb: t.Disk,
	}
	if t.Entrypoint != nil {
		out.DockerEntrypoint = *t.Entrypoint
	}
	if t.Cmd != nil {
		out.DockerStartCmd = *t.Cmd
	}
	if len(t.Env) > 0 {
		out.Env = t.Env
	}
	if t.Registry != nil {
		out.ContainerRegistryAuthID = *t.Registry
	}
	if p := t.Mounts.Persistent; p != nil {
		out.VolumeInGb = p.Size
		out.VolumeMountPath = p.Path
	}
	return out
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func podMemoryAndVcpu(p types.Pod) (memory, vcpu int) {
	switch {
	case p.Gpu != nil:
		return int(p.Gpu.Memory), int(p.Gpu.VcpuCount)
	case p.Cpu != nil:
		return p.Cpu.Memory, p.Cpu.VcpuCount
	}
	return 0, 0
}

func podVolume(p types.Pod) (sizeGb int, mountPath, networkVolumeID string) {
	if m := p.Mounts.Persistent; m != nil {
		sizeGb, mountPath = m.Size, m.Path
	}
	if p.Mounts.Network != nil && len(*p.Mounts.Network) > 0 {
		network := (*p.Mounts.Network)[0]
		networkVolumeID = network.VolumeId
		if mountPath == "" {
			mountPath = network.Path
		}
	}
	return sizeGb, mountPath, networkVolumeID
}

// podFromSDK converts a rest v2 pod read, carrying the runtime/ssh view of the
// same read along (Pod.Legacy).
//
// BACKWARD_COMPATIBILITY: prints the field names v1 did (desiredStatus mapped
// from v2's status, imageName, costPerHr, createdAt in Go's time.String
// layout, ...). Next major: print the API's pod.
func podFromSDK(p types.Pod) Pod {
	memory, vcpu := podMemoryAndVcpu(p)
	volumeInGb, mountPath, networkVolumeID := podVolume(p)
	pod := Pod{
		ID:                p.Id,
		Name:              p.Name,
		DesiredStatus:     legacyDesiredStatus(string(p.Status)),
		CreatedAt:         legacyTimestamp(p.CreatedAt),
		ImageName:         p.Image,
		VolumeInGb:        volumeInGb,
		ContainerDiskInGb: p.Disk,
		MemoryInGb:        memory,
		VcpuCount:         vcpu,
		VolumeMountPath:   mountPath,
		Ports:             p.Ports,
		DockerStartCmd:    deref(p.Cmd),
		DockerEntrypoint:  deref(p.Entrypoint),
		CostPerHr:         float32To64(p.Cost),
		NetworkVolumeID:   networkVolumeID,
		legacy:            legacyPodFromSDK(p),
	}
	if p.Gpu != nil {
		pod.GpuTypeID = p.Gpu.Id
		pod.GpuCount = deref(p.Gpu.Count)
	}
	if len(p.Env) > 0 {
		pod.Env = p.Env
	}
	return pod
}

// legacyPodFromSDK converts a rest v2 pod read into the runtime/ssh view that
// internal/sshconnect, internal/podstate and the deprecated pod commands use.
//
// BACKWARD_COMPATIBILITY: the graphql-era shape (env as KEY=VALUE, ports as a
// comma-separated string, isIpPublic per runtime port). Next major: use the
// API's pod.
func legacyPodFromSDK(p types.Pod) *LegacyPod {
	memory, vcpu := podMemoryAndVcpu(p)
	volumeInGb, mountPath, _ := podVolume(p)
	legacy := &LegacyPod{
		ID:                p.Id,
		ContainerDiskInGb: p.Disk,
		CostPerHr:         p.Cost,
		DesiredStatus:     legacyDesiredStatus(string(p.Status)),
		ImageName:         p.Image,
		MemoryInGb:        memory,
		Name:              p.Name,
		Ports:             strings.Join(p.Ports, ","),
		VcpuCount:         vcpu,
		VolumeInGb:        volumeInGb,
		VolumeMountPath:   mountPath,
	}
	if p.Gpu != nil {
		legacy.GpuCount = deref(p.Gpu.Count)
	}
	for k, v := range p.Env {
		legacy.Env = append(legacy.Env, k+"="+v)
	}
	if p.Runtime != nil {
		runtime := &LegacyRuntime{UptimeInSeconds: p.Runtime.Uptime, Ports: []*LegacyPort{}}
		for _, port := range deref(p.Runtime.Ports) {
			lp := &LegacyPort{PrivatePort: deref(port.Private), PortType: deref(port.Type), Ip: deref(port.Ip), PublicPort: deref(port.Public)}
			// reachable means a public address and a published port; a port
			// v2 lists before it publishes one is not an endpoint yet. any
			// type, as graphql's isIpPublic was; ssh checks for tcp itself
			lp.IsIpPublic = port.Ip != nil && port.Public != nil && *port.Public > 0 && isPublicIP(*port.Ip)
			runtime.Ports = append(runtime.Ports, lp)
		}
		legacy.Runtime = runtime
	}
	return legacy
}
