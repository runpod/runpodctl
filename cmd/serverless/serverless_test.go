package serverless

import (
	"bytes"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/api"

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
}

func TestCreateCmd_RejectsHubWithModelReference(t *testing.T) {
	oldTemplateID := createTemplateID
	oldHubID := createHubID
	oldModelReferences := createModelReferences
	t.Cleanup(func() {
		createTemplateID = oldTemplateID
		createHubID = oldHubID
		createModelReferences = oldModelReferences
	})

	createTemplateID = ""
	createHubID = "hub-123"
	createModelReferences = []string{"https://local/user/model:hash"}

	err := runCreate(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--model-reference is only supported with --template-id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateCmd_RejectsCPUWithModelReference(t *testing.T) {
	oldTemplateID := createTemplateID
	oldHubID := createHubID
	oldComputeType := createComputeType
	oldModelReferences := createModelReferences
	t.Cleanup(func() {
		createTemplateID = oldTemplateID
		createHubID = oldHubID
		createComputeType = oldComputeType
		createModelReferences = oldModelReferences
	})

	createTemplateID = "tpl-123"
	createHubID = ""
	createComputeType = "CPU"
	createModelReferences = []string{"https://local/user/model:hash"}

	err := runCreate(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--model-reference is only supported with --compute-type GPU") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildTemplateEndpointGQLInput(t *testing.T) {
	modelReferences := []string{"https://local/user/model:hash"}
	gqlReq := buildTemplateEndpointGQLInput(&api.EndpointCreateRequest{
		Name:            "test-endpoint",
		TemplateID:      "tpl-123",
		GpuCount:        2,
		WorkersMin:      1,
		WorkersMax:      3,
		NetworkVolumeID: "vol-123",
	}, "ADA_24", "US-KS-2", modelReferences)

	if gqlReq.Name != "test-endpoint" {
		t.Errorf("expected name test-endpoint, got %s", gqlReq.Name)
	}
	if gqlReq.TemplateID != "tpl-123" {
		t.Errorf("expected template id tpl-123, got %s", gqlReq.TemplateID)
	}
	if gqlReq.GpuIDs != "ADA_24" {
		t.Errorf("expected gpu ids ADA_24, got %s", gqlReq.GpuIDs)
	}
	if gqlReq.GpuCount != 2 || gqlReq.WorkersMin != 1 || gqlReq.WorkersMax != 3 {
		t.Fatalf("unexpected worker/gpu settings: %#v", gqlReq)
	}
	if gqlReq.Locations != "US-KS-2" {
		t.Errorf("expected locations US-KS-2, got %s", gqlReq.Locations)
	}
	if gqlReq.NetworkVolumeID != "vol-123" {
		t.Errorf("expected network volume id vol-123, got %s", gqlReq.NetworkVolumeID)
	}
	if len(gqlReq.ModelReferences) != 1 || gqlReq.ModelReferences[0] != modelReferences[0] {
		t.Fatalf("unexpected model references: %#v", gqlReq.ModelReferences)
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
	if flags.Lookup("scaler-type") == nil {
		t.Error("expected --scaler-type flag")
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
