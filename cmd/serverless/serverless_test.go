package serverless

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestServerlessCmd_Structure(t *testing.T) {
	if Cmd.Use != "serverless" {
		t.Errorf("expected use 'serverless', got %s", Cmd.Use)
	}

	// check alias is only sls
	if len(Cmd.Aliases) != 1 {
		t.Errorf("expected exactly 1 alias, got %d", len(Cmd.Aliases))
	}
	if Cmd.Aliases[0] != "sls" {
		t.Errorf("expected alias 'sls', got %s", Cmd.Aliases[0])
	}

	// check subcommands exist
	expectedSubcommands := []string{"list", "get <endpoint-id>", "create", "update <endpoint-id>", "delete <endpoint-id>"}
	for _, expected := range expectedSubcommands {
		found := false
		for _, cmd := range Cmd.Commands() {
			if cmd.Use == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected subcommand %s not found", expected)
		}
	}
}

func TestListCmd_Flags(t *testing.T) {
	flags := listCmd.Flags()

	if flags.Lookup("include-template") == nil {
		t.Error("expected --include-template flag")
	}
	if flags.Lookup("include-workers") == nil {
		t.Error("expected --include-workers flag")
	}
}

func TestCreateCmd_Flags(t *testing.T) {
	flags := createCmd.Flags()

	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("template-id") == nil {
		t.Error("expected --template-id flag")
	}
	if flags.Lookup("gpu-id") == nil {
		t.Error("expected --gpu-id flag")
	}
	if flags.Lookup("workers-min") == nil {
		t.Error("expected --workers-min flag")
	}
	if flags.Lookup("workers-max") == nil {
		t.Error("expected --workers-max flag")
	}
	if flags.Lookup("model-reference") == nil {
		t.Error("expected --model-reference flag")
	}
	if flags.Lookup("compute-type") == nil {
		t.Error("expected --compute-type flag")
	}
	if flags.Lookup("instance-id") == nil {
		t.Error("expected --instance-id flag")
	}
	if flags.Lookup("network-volume-ids") == nil {
		t.Error("expected --network-volume-ids flag")
	}
}

// snapshotCreateFlags restores every serverless-create global after a test that
// mutates them, so package-level state doesn't leak between tests, then sets a
// known-good baseline. individual tests override only what they exercise.
func snapshotCreateFlags(t *testing.T) {
	t.Helper()
	old := struct {
		name, templateID, hubID, computeType, gpuID, instanceID string
		dataCenterIDs, networkVolumeID, networkVolumeIDs        string
		minCudaVersion, scaleBy                                 string
		gpuCount, workersMin, workersMax                        int
		scaleThreshold, idleTimeout, executionTimeout           int
		flashBoot                                               bool
		envVars, modelReferences                                []string
	}{
		createName, createTemplateID, createHubID, createComputeType, createGpuTypeID, createInstanceID,
		createDataCenterIDs, createNetworkVolumeID, createNetworkVolumeIDs,
		createMinCudaVersion, createScaleBy,
		createGpuCount, createWorkersMin, createWorkersMax,
		createScaleThreshold, createIdleTimeout, createExecutionTimeout,
		createFlashBoot,
		createEnvVars, createModelReferences,
	}
	t.Cleanup(func() {
		createName, createTemplateID, createHubID = old.name, old.templateID, old.hubID
		createComputeType, createGpuTypeID, createInstanceID = old.computeType, old.gpuID, old.instanceID
		createDataCenterIDs, createNetworkVolumeID, createNetworkVolumeIDs = old.dataCenterIDs, old.networkVolumeID, old.networkVolumeIDs
		createMinCudaVersion, createScaleBy = old.minCudaVersion, old.scaleBy
		createGpuCount, createWorkersMin, createWorkersMax = old.gpuCount, old.workersMin, old.workersMax
		createScaleThreshold, createIdleTimeout, createExecutionTimeout = old.scaleThreshold, old.idleTimeout, old.executionTimeout
		createFlashBoot = old.flashBoot
		createEnvVars, createModelReferences = old.envVars, old.modelReferences
	})
	// known-good baseline matching the flag defaults; tests override per case.
	createName, createTemplateID, createHubID = "", "tpl-123", ""
	createComputeType, createGpuTypeID, createInstanceID = "GPU", "", ""
	createDataCenterIDs, createNetworkVolumeID, createNetworkVolumeIDs = "", "", ""
	createMinCudaVersion, createScaleBy = "", ""
	createGpuCount, createWorkersMin, createWorkersMax = 1, 0, 3
	createScaleThreshold, createIdleTimeout, createExecutionTimeout = -1, -1, -1
	createFlashBoot = true
	createEnvVars, createModelReferences = nil, nil
}

// these validations all run before any api client/network call, so they're
// safe to exercise without hitting the live api.
func TestCreateCmd_Validations(t *testing.T) {
	cases := []struct {
		name    string
		setup   func()
		wantErr string
	}{
		{
			name:    "invalid compute type",
			setup:   func() { createComputeType = "TPU" },
			wantErr: "invalid --compute-type",
		},
		{
			name:    "cpu with gpu-id",
			setup:   func() { createComputeType = "CPU"; createGpuTypeID = "NVIDIA A40" },
			wantErr: "--gpu-id must be empty when --compute-type is CPU",
		},
		{
			name:    "gpu with instance-id",
			setup:   func() { createComputeType = "GPU"; createInstanceID = "cpu3g-4-16" },
			wantErr: "--instance-id is only supported with --compute-type CPU",
		},
		{
			name:    "both network volume flags",
			setup:   func() { createNetworkVolumeID = "vol-1"; createNetworkVolumeIDs = "vol-2,vol-3" },
			wantErr: "--network-volume-id and --network-volume-ids are mutually exclusive",
		},
		{
			name: "hub with model reference",
			setup: func() {
				createTemplateID = ""
				createHubID = "hub-1"
				createModelReferences = []string{"https://x/y:z"}
			},
			wantErr: "--model-reference is only supported with --template-id",
		},
		{
			name:    "cpu with model reference",
			setup:   func() { createComputeType = "CPU"; createModelReferences = []string{"https://x/y:z"} },
			wantErr: "--model-reference is only supported with --compute-type GPU",
		},
		{
			name:    "name too short",
			setup:   func() { createName = "ab" },
			wantErr: "--name must be at least 3 characters",
		},
		{
			name:    "scale-threshold below 1",
			setup:   func() { createScaleThreshold = 0 },
			wantErr: "--scale-threshold must be at least 1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshotCreateFlags(t)
			tc.setup()
			err := runCreate(&cobra.Command{}, nil)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestUpdateCmd_Flags(t *testing.T) {
	flags := updateCmd.Flags()

	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("workers-min") == nil {
		t.Error("expected --workers-min flag")
	}
	if flags.Lookup("workers-max") == nil {
		t.Error("expected --workers-max flag")
	}
	if flags.Lookup("idle-timeout") == nil {
		t.Error("expected --idle-timeout flag")
	}
	if flags.Lookup("scale-by") == nil {
		t.Error("expected --scale-by flag")
	}
	if flags.Lookup("scale-threshold") == nil {
		t.Error("expected --scale-threshold flag")
	}
}

func TestDeleteCmd_Aliases(t *testing.T) {
	aliases := deleteCmd.Aliases
	hasRm := false
	hasRemove := false
	for _, alias := range aliases {
		if alias == "rm" {
			hasRm = true
		}
		if alias == "remove" {
			hasRemove = true
		}
	}
	if !hasRm {
		t.Error("expected alias 'rm'")
	}
	if !hasRemove {
		t.Error("expected alias 'remove'")
	}
}

func executeCommand(root *cobra.Command, args ...string) (output string, err error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err = root.Execute()
	return buf.String(), err
}

func TestServerlessCmd_Help(t *testing.T) {
	output, err := executeCommand(Cmd, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output == "" {
		t.Error("expected help output")
	}
}
