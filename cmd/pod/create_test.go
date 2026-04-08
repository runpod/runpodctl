package pod

import "testing"

func TestCreateCmd_HasMinCudaVersionFlag(t *testing.T) {
	flag := createCmd.Flags().Lookup("min-cuda-version")
	if flag == nil {
		t.Fatal("expected min-cuda-version flag")
	}
}

func TestBuildCreatePodGQLRequest_IncludesMinCudaVersion(t *testing.T) {
	origName := createName
	origImage := createImageName
	origTemplateID := createTemplateID
	origMinCudaVersion := createMinCudaVersion
	origGpuCount := createGpuCount
	origVolumeInGb := createVolumeInGb
	origContainerDiskInGb := createContainerDiskInGb
	origVolumeMountPath := createVolumeMountPath
	origSSH := createSSH
	origPorts := createPorts
	origEnv := createEnv
	origDataCenterIDs := createDataCenterIDs
	origNetworkVolumeID := createNetworkVolumeID

	t.Cleanup(func() {
		createName = origName
		createImageName = origImage
		createTemplateID = origTemplateID
		createMinCudaVersion = origMinCudaVersion
		createGpuCount = origGpuCount
		createVolumeInGb = origVolumeInGb
		createContainerDiskInGb = origContainerDiskInGb
		createVolumeMountPath = origVolumeMountPath
		createSSH = origSSH
		createPorts = origPorts
		createEnv = origEnv
		createDataCenterIDs = origDataCenterIDs
		createNetworkVolumeID = origNetworkVolumeID
	})

	createName = "cuda-pod"
	createImageName = "runpod/test"
	createTemplateID = ""
	createMinCudaVersion = "12.6"
	createGpuCount = 1
	createVolumeInGb = 50
	createContainerDiskInGb = 25
	createVolumeMountPath = "/workspace"
	createSSH = true
	createPorts = "22/tcp"
	createEnv = `{"A":"1"}`
	createDataCenterIDs = "DC-1,DC-2"
	createNetworkVolumeID = "nv-123"

	req, err := buildCreatePodGQLRequest("NVIDIA GeForce RTX 4090", "SECURE", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.MinCudaVersion != "12.6" {
		t.Fatalf("expected min cuda version, got %q", req.MinCudaVersion)
	}
	if req.DataCenterId != "DC-1" {
		t.Fatalf("expected first data center id, got %q", req.DataCenterId)
	}
	if req.NetworkVolumeId != "nv-123" {
		t.Fatalf("expected network volume id, got %q", req.NetworkVolumeId)
	}
	if len(req.Env) != 1 || req.Env[0].Key != "A" || req.Env[0].Value != "1" {
		t.Fatalf("unexpected env payload: %#v", req.Env)
	}
}

func TestRunCreate_RejectsMinCudaVersionForCPU(t *testing.T) {
	origTemplateID := createTemplateID
	origImage := createImageName
	origComputeType := createComputeType
	origGpuTypeID := createGpuTypeID
	origMinCudaVersion := createMinCudaVersion

	t.Cleanup(func() {
		createTemplateID = origTemplateID
		createImageName = origImage
		createComputeType = origComputeType
		createGpuTypeID = origGpuTypeID
		createMinCudaVersion = origMinCudaVersion
	})

	createTemplateID = ""
	createImageName = "ubuntu:22.04"
	createComputeType = "CPU"
	createGpuTypeID = ""
	createMinCudaVersion = "12.6"

	err := runCreate(createCmd, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "--min-cuda-version is only supported for compute type GPU" {
		t.Fatalf("unexpected error: %v", err)
	}
}
