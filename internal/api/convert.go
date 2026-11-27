package api

import (
	"strconv"

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
