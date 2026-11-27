package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/runpod/go-sdk/v2/lib/types"
)

// requireAllFieldsSet fails for every exported field of v, recursively, that
// holds its zero value, except the dotted paths in allowZero. A converter test
// feeds a fully populated SDK value, so a zero field means a mapping was lost.
func requireAllFieldsSet(t *testing.T, v any, allowZero ...string) {
	t.Helper()
	allowed := map[string]bool{}
	for _, p := range allowZero {
		allowed[p] = true
	}
	var walk func(path string, v reflect.Value)
	walk = func(path string, v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				walk(path, v.Elem())
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				walk(path, v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(path, v.MapIndex(k))
			}
		case reflect.Struct:
			typ := v.Type()
			for i := range typ.NumField() {
				f := typ.Field(i)
				if !f.IsExported() || f.Tag.Get("json") == "-" {
					continue
				}
				p := strings.TrimPrefix(path+"."+f.Name, ".")
				if v.Field(i).IsZero() {
					if !allowed[p] {
						t.Errorf("%s is zero after conversion", p)
					}
					continue
				}
				walk(p, v.Field(i))
			}
		}
	}
	walk("", reflect.ValueOf(v))
}

func TestNetworkVolumeFromSDK(t *testing.T) {
	got := networkVolumeFromSDK(types.NetworkVolume{
		Id: "vol-abc123", Name: "models", Size: 50, DataCenter: "EU-RO-1", Type: "STANDARD",
	})
	requireAllFieldsSet(t, got)
	want := NetworkVolume{ID: "vol-abc123", Name: "models", Size: 50, DataCenterID: "EU-RO-1"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFloat32To64KeepsDecimalValue(t *testing.T) {
	for in, want := range map[float32]float64{0.69: 0.69, 2.49: 2.49, 0.0001: 0.0001, 1234.5: 1234.5, 0: 0} {
		if got := float32To64(in); got != want {
			t.Errorf("float32To64(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestGpuTypeFromSDK(t *testing.T) {
	high, low := types.AvailabilityLevel("HIGH"), types.AvailabilityLevel("LOW")
	gpu := types.GpuType{
		Id: "NVIDIA GeForce RTX 4090", Name: "RTX 4090", Memory: 24, Secure: true, Community: true,
		Availability: &low,
		DataCenters: &[]types.DataCenterAvailability{
			{Id: "EU-RO-1", Availability: high},
			{Id: "US-TX-3", Availability: "NONE"},
		},
	}
	gpu.Price.Secure, gpu.Price.Community = 0.69, 0.34

	got := gpuTypeFromSDK(gpu)
	requireAllFieldsSet(t, got)
	want := GpuTypeWithAvailability{
		GpuType: GpuType{
			ID: "NVIDIA GeForce RTX 4090", DisplayName: "RTX 4090", MemoryInGb: 24,
			SecureCloud: true, CommunityCloud: true, SecurePrice: 0.69, CommunityPrice: 0.34,
		},
		StockStatus: "High", // the best data center beats the top level
		Available:   true,
		DataCenterAvailability: []GpuDataCenterAvailability{
			{DataCenterID: "EU-RO-1", StockStatus: "High"},
			{DataCenterID: "US-TX-3", StockStatus: "none"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestDataCenterFromSDK(t *testing.T) {
	got := dataCenterFromSDK(types.DataCenter{
		Id: "EU-RO-1", Name: "Europe Romania 1", Region: "EUROPE",
		GpuAvailability: &[]types.CatalogResourceAvailability{
			{Id: "NVIDIA A40", Name: "A40", Availability: "MEDIUM"},
		},
	})
	requireAllFieldsSet(t, got)
	want := DataCenter{
		ID: "EU-RO-1", Name: "Europe Romania 1", Location: "EUROPE",
		GpuAvailability: []GpuAvailabilityInDataCenter{{GpuTypeID: "NVIDIA A40", DisplayName: "A40", StockStatus: "Medium"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestRegistryAuthFromSDK(t *testing.T) {
	got := registryAuthFromSDK(types.Registry{Id: "reg-abc123", Name: "my-registry"})
	requireAllFieldsSet(t, got)
	if want := (ContainerRegistryAuth{ID: "reg-abc123", Name: "my-registry"}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestTemplateFromSDK(t *testing.T) {
	registry := "reg-abc123"
	got := templateFromSDK(types.Template{
		Id: "tpl-abc123", Name: "pytorch-dev", Image: "runpod/pytorch:2.1.0", Serverless: true, Public: true,
		Category: "NVIDIA", Ports: []string{"8888/http"}, Disk: 20, Registry: &registry,
		Entrypoint: &[]string{"bash"}, Cmd: &[]string{"-c", "run"}, Env: map[string]string{"HF_HOME": "/workspace/hf"},
		Mounts: types.TemplateMounts{Persistent: &types.PersistentMount{Size: 50, Path: "/workspace"}},
	})
	// isRunpod, portsConfig and readme have no v2 field; they come from graphql
	requireAllFieldsSet(t, got, "IsRunpod", "PortsConfig", "Readme")
	want := Template{
		ID: "tpl-abc123", Name: "pytorch-dev", ImageName: "runpod/pytorch:2.1.0", IsServerless: true, IsPublic: true,
		Category: "NVIDIA", Ports: []string{"8888/http"}, ContainerDiskInGb: 20, ContainerRegistryAuthID: "reg-abc123",
		DockerEntrypoint: []string{"bash"}, DockerStartCmd: []string{"-c", "run"}, Env: map[string]string{"HF_HOME": "/workspace/hf"},
		VolumeInGb: 50, VolumeMountPath: "/workspace",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func sdkTestPod() types.Pod {
	count, uptime, private, public := 2, 3600, 22, 40022
	ip, tcp, dc := "203.0.113.10", "tcp", "EU-RO-1"
	return types.Pod{
		Id: "pod-abc123", Name: "my-pod", Status: "STARTING", Image: "runpod/pytorch:2.1.0",
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 123000000, time.UTC),
		Gpu:       &types.GpuConfig{Id: "NVIDIA A40", Count: &count, Memory: 62.5, VcpuCount: 16},
		Disk:      50, Ports: []string{"8888/http", "22/tcp"}, Cost: 0.69, DataCenterId: &dc,
		Cmd: &[]string{"sleep", "infinity"}, Entrypoint: &[]string{"/bin/bash", "-c"},
		Env: map[string]string{"HF_HOME": "/workspace/hf"},
		Mounts: types.Mounts{
			Persistent: &types.PersistentMount{Size: 20, Path: "/workspace"},
			Network:    &[]types.NetworkMount{{VolumeId: "vol-abc123", Path: "/data"}},
		},
		Runtime: &types.PodRuntime{Uptime: &uptime, Ports: &[]types.PodRuntimePort{{Ip: &ip, Private: &private, Public: &public, Type: &tcp}}},
	}
}

func TestPodFromSDK(t *testing.T) {
	got := podFromSDK(sdkTestPod())
	// set by GetPod's options, or never reported by v2
	requireAllFieldsSet(t, got, "UptimeSeconds", "NetworkVolume", "Machine", "Runtime")
	want := Pod{
		ID: "pod-abc123", Name: "my-pod", DesiredStatus: "RUNNING", CreatedAt: "2026-06-01 12:00:00.123 +0000 UTC",
		ImageName: "runpod/pytorch:2.1.0", GpuTypeID: "NVIDIA A40", GpuCount: 2, VolumeInGb: 20, ContainerDiskInGb: 50,
		MemoryInGb: 62, VcpuCount: 16, VolumeMountPath: "/workspace", Ports: []string{"8888/http", "22/tcp"},
		DockerStartCmd: []string{"sleep", "infinity"}, DockerEntrypoint: []string{"/bin/bash", "-c"}, CostPerHr: 0.69,
		NetworkVolumeID: "vol-abc123", Env: map[string]string{"HF_HOME": "/workspace/hf"},
	}
	got.legacy = nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLegacyPodFromSDK(t *testing.T) {
	got := legacyPodFromSDK(sdkTestPod())
	// set by GetLegacyPods only
	requireAllFieldsSet(t, got, "PodType", "Machine")
	if got.CostPerHr != float32(0.69) || got.Env[0] != "HF_HOME=/workspace/hf" || got.Ports != "8888/http,22/tcp" || got.MemoryInGb != 62 {
		t.Fatalf("got %+v", got)
	}
	port := got.Runtime.Ports[0]
	if want := (LegacyPort{Ip: "203.0.113.10", IsIpPublic: true, PrivatePort: 22, PublicPort: 40022, PortType: "tcp"}); *port != want {
		t.Fatalf("port = %+v, want %+v", *port, want)
	}
}

func TestPodFromSDKCPUAndMissingTime(t *testing.T) {
	got := podFromSDK(types.Pod{Id: "pod-def456", Status: "EXITED", Cpu: &types.CpuConfig{Id: "cpu3c", Memory: 4, VcpuCount: 2}})
	if got.MemoryInGb != 4 || got.VcpuCount != 2 || got.CreatedAt != nil || got.DesiredStatus != "EXITED" || got.GpuTypeID != "" {
		t.Fatalf("got %+v", got)
	}
}

func sdkTestEndpoint(t *testing.T, scaling string) types.Endpoint {
	t.Helper()
	var e types.Endpoint
	body := `{"id":"ep-abc123","name":"my-endpoint","image":"runpod/worker-vllm:stable","args":"python handler.py",
		"cmd":["python","handler.py"],"env":{"HF_HOME":"/workspace/hf"},"ports":["8000/http"],"disk":20,"registry":"reg-abc123",
		"gpu":{"pools":["AMPERE_48","ADA_24"],"excludedTypes":["NVIDIA RTX A6000"],"count":2,"minCudaVersion":"12.4","allowedCudaVersions":[]},
		"networkVolumes":["vol-abc123","vol-def456"],"dataCenterIds":["EU-RO-1","US-TX-3"],"flashboot":"FLASHBOOT","timeout":600000,
		"scaling":` + scaling + `,"workers":{"min":1,"max":3,"idleTimeout":5},"createdAt":"2026-03-14T08:30:00Z","mounts":{"global":[],"network":[]}}`
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEndpointFromSDK(t *testing.T) {
	t.Setenv("RUNPOD_INVOKE_URL", "")
	pools := map[string][]string{"AMPERE_48": {"NVIDIA A40", "NVIDIA RTX A6000"}}
	got := endpointFromSDK(sdkTestEndpoint(t, `{"type":"QUEUE_DELAY","queueDelay":2.7}`), pools)
	// graphql-only fields, and the --include-template/--include-workers expansions
	requireAllFieldsSet(t, got, "TemplateID", "GpuIDs", "InstanceIDs", "FlashBootType", "ComputeType", "ModelReferences",
		"Template", "Workers", "NetworkVolumeIDs.DataCenterID")
	if got.ScalerValue != 2.7 || got.ScalerType != "QUEUE_DELAY" {
		t.Errorf("scaling = %s %v, want QUEUE_DELAY 2.7 (no float32 noise)", got.ScalerType, got.ScalerValue)
	}
	// ADA_24 is not in the catalog, so it prints as its id; the excluded type is dropped
	if want := []string{"NVIDIA A40", "ADA_24"}; !reflect.DeepEqual(got.GpuTypeIDs, want) {
		t.Errorf("gpuTypeIds = %v, want %v", got.GpuTypeIDs, want)
	}
	if got.Locations != "EU-RO-1,US-TX-3" || got.NetworkVolumeID != "vol-abc123" || !*got.Flashboot || *got.IdleTimeout != 5 {
		t.Errorf("got %+v", got)
	}

	requestCount := endpointFromSDK(sdkTestEndpoint(t, `{"type":"REQUEST_COUNT","requestCount":4}`), pools)
	if requestCount.ScalerValue != 4 || requestCount.ScalerType != "REQUEST_COUNT" {
		t.Errorf("scaling = %s %v, want REQUEST_COUNT 4", requestCount.ScalerType, requestCount.ScalerValue)
	}
}

func TestEndpointInlineTemplate(t *testing.T) {
	got := endpointInlineTemplate(sdkTestEndpoint(t, `{"type":"QUEUE_DELAY","queueDelay":4}`))
	want := map[string]any{
		"imageName": "runpod/worker-vllm:stable", "containerDiskInGb": 20, "dockerArgs": "python handler.py",
		"env": map[string]string{"HF_HOME": "/workspace/hf"}, "ports": "8000/http", "isServerless": true,
		"containerRegistryAuthId": "reg-abc123", "dockerStartCmd": []string{"python", "handler.py"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}
