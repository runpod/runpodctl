package api

import (
	"reflect"
	"strings"
	"testing"

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
