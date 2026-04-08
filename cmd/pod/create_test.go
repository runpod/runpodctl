package pod

import "testing"

func TestCreateCmd_HasBidPerGPUFlag(t *testing.T) {
	if createCmd.Flags().Lookup("bid-per-gpu") == nil {
		t.Fatal("expected --bid-per-gpu flag")
	}
}

func TestRunCreate_RejectsBidPerGPUForCPU(t *testing.T) {
	origTemplateID := createTemplateID
	origImageName := createImageName
	origComputeType := createComputeType
	origBidPerGPU := createBidPerGPU

	t.Cleanup(func() {
		createTemplateID = origTemplateID
		createImageName = origImageName
		createComputeType = origComputeType
		createBidPerGPU = origBidPerGPU
	})

	createTemplateID = ""
	createImageName = "ubuntu:22.04"
	createComputeType = "CPU"
	createBidPerGPU = 0.2

	err := runCreate(createCmd, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "--bid-per-gpu is only supported for compute type GPU" {
		t.Fatalf("unexpected error: %v", err)
	}
}
