package api

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestHubReleaseConfigDeploymentConstraints(t *testing.T) {
	var config HubReleaseConfig
	err := json.Unmarshal([]byte(`{
		"runsOn":"GPU",
		"containerDiskInGb":150,
		"gpuIds":"ADA_80_PRO,AMPERE_80",
		"gpuCount":2,
		"allowedCudaVersions":["13.0","12.8"]
	}`), &config)
	if err != nil {
		t.Fatalf("unmarshal hub config: %v", err)
	}

	if config.RunsOn != "GPU" || config.ContainerDiskInGb != 150 || config.GpuIDs != "ADA_80_PRO,AMPERE_80" || config.GpuCount != 2 {
		t.Fatalf("unexpected deployment constraints: %#v", config)
	}
	if want := []string{"13.0", "12.8"}; !reflect.DeepEqual(config.AllowedCudaVersions, want) {
		t.Fatalf("allowed cuda versions = %#v, want %#v", config.AllowedCudaVersions, want)
	}
}
