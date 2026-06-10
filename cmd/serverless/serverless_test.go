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

// TestCreateCmd_FlagValidation exercises runCreate's flag-validation guards
// (the only logic that runs before the API client is constructed). Each row
// must fail with a specific error before any network call is attempted.
func TestCreateCmd_FlagValidation(t *testing.T) {
	type fields struct {
		templateID      string
		hubID           string
		computeType     string
		modelReferences []string
	}

	tests := []struct {
		name       string
		fields     fields
		wantErrSub string
	}{
		{
			name: "negative: template and hub both set",
			fields: fields{
				templateID: "tpl-1",
				hubID:      "hub-1",
			},
			wantErrSub: "mutually exclusive",
		},
		{
			name: "negative: hub + model-reference",
			fields: fields{
				hubID:           "hub-1",
				modelReferences: []string{"hf://m"},
			},
			wantErrSub: "--model-reference is only supported with --template-id",
		},
		{
			name: "negative: CPU + model-reference (lowercase)",
			fields: fields{
				templateID:      "tpl-1",
				computeType:     "cpu",
				modelReferences: []string{"hf://m"},
			},
			wantErrSub: "--model-reference is only supported with --compute-type GPU",
		},
		{
			name: "boundary: whitespace-only compute-type is treated as unspecified",
			fields: fields{
				templateID:      "tpl-1",
				computeType:     "   ",
				modelReferences: []string{"hf://m"},
			},
			// "" / whitespace passes the compute-type guard; the test should NOT
			// fail on the compute-type message. It WILL fail later when the API
			// client is constructed without RUNPOD_API_KEY in env, so we assert
			// the error is not the compute-type guard's message.
			wantErrSub: "",
		},
		{
			name: "corner: explicit GPU + model-reference passes validation",
			fields: fields{
				templateID:      "tpl-1",
				computeType:     "GPU",
				modelReferences: []string{"hf://m"},
			},
			wantErrSub: "",
		},
		{
			name: "corner: mixed-case GPU normalises to GPU",
			fields: fields{
				templateID:      "tpl-1",
				computeType:     "gPu",
				modelReferences: []string{"hf://m"},
			},
			wantErrSub: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			createTemplateID = tt.fields.templateID
			createHubID = tt.fields.hubID
			createComputeType = tt.fields.computeType
			createModelReferences = tt.fields.modelReferences

			err := runCreate(&cobra.Command{}, nil)

			if tt.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSub)
				}
				if !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErrSub, err)
				}
				return
			}

			// rows with wantErrSub == "" should pass the guard layer; any error
			// returned must come from a *later* layer (e.g. api.NewClient or the
			// API call), not from the guard itself. Asserting absence of the
			// guard-layer substrings keeps the test resilient to env differences.
			if err != nil {
				for _, guardMsg := range []string{
					"mutually exclusive",
					"--model-reference is only supported with --template-id",
					"--model-reference is only supported with --compute-type GPU",
				} {
					if strings.Contains(err.Error(), guardMsg) {
						t.Fatalf("guard %q tripped unexpectedly: %v", guardMsg, err)
					}
				}
			}
		})
	}
}

func TestBuildTemplateEndpointGQLInput(t *testing.T) {
	tests := []struct {
		name            string
		req             *api.EndpointCreateRequest
		gpuTypeID       string
		locations       string
		modelReferences []string
		want            api.EndpointCreateGQLInput
	}{
		{
			name: "positive: all fields populated",
			req: &api.EndpointCreateRequest{
				Name:            "test-endpoint",
				TemplateID:      "tpl-123",
				GpuCount:        2,
				WorkersMin:      1,
				WorkersMax:      3,
				NetworkVolumeID: "vol-123",
			},
			gpuTypeID:       "ADA_24",
			locations:       "US-KS-2",
			modelReferences: []string{"https://local/user/model:hash"},
			want: api.EndpointCreateGQLInput{
				Name:            "test-endpoint",
				TemplateID:      "tpl-123",
				GpuIDs:          "ADA_24",
				GpuCount:        2,
				WorkersMin:      1,
				WorkersMax:      3,
				Locations:       "US-KS-2",
				NetworkVolumeID: "vol-123",
				ModelReferences: []string{"https://local/user/model:hash"},
			},
		},
		{
			name: "boundary: zero workers and zero gpu count",
			req: &api.EndpointCreateRequest{
				Name:       "ep",
				TemplateID: "tpl-z",
			},
			gpuTypeID:       "L40",
			locations:       "",
			modelReferences: []string{"hf://m"},
			want: api.EndpointCreateGQLInput{
				Name:            "ep",
				TemplateID:      "tpl-z",
				GpuIDs:          "L40",
				ModelReferences: []string{"hf://m"},
			},
		},
		{
			name: "corner: multiple model references preserved in order",
			req: &api.EndpointCreateRequest{
				TemplateID: "tpl-multi",
			},
			gpuTypeID: "A100",
			modelReferences: []string{
				"hf://a:v1",
				"hf://b:v2",
				"hf://c:v3",
			},
			want: api.EndpointCreateGQLInput{
				TemplateID:      "tpl-multi",
				GpuIDs:          "A100",
				ModelReferences: []string{"hf://a:v1", "hf://b:v2", "hf://c:v3"},
			},
		},
		{
			name:            "corner: empty input + nil model refs",
			req:             &api.EndpointCreateRequest{},
			gpuTypeID:       "",
			locations:       "",
			modelReferences: nil,
			want:            api.EndpointCreateGQLInput{},
		},
		{
			name: "negative: name omitted is empty (server auto-generates)",
			req: &api.EndpointCreateRequest{
				TemplateID: "tpl-noname",
			},
			gpuTypeID:       "RTX4090",
			modelReferences: []string{"hf://m"},
			want: api.EndpointCreateGQLInput{
				TemplateID:      "tpl-noname",
				GpuIDs:          "RTX4090",
				ModelReferences: []string{"hf://m"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTemplateEndpointGQLInput(tt.req, tt.gpuTypeID, tt.locations, tt.modelReferences)
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.TemplateID != tt.want.TemplateID {
				t.Errorf("TemplateID = %q, want %q", got.TemplateID, tt.want.TemplateID)
			}
			if got.GpuIDs != tt.want.GpuIDs {
				t.Errorf("GpuIDs = %q, want %q", got.GpuIDs, tt.want.GpuIDs)
			}
			if got.GpuCount != tt.want.GpuCount {
				t.Errorf("GpuCount = %d, want %d", got.GpuCount, tt.want.GpuCount)
			}
			if got.WorkersMin != tt.want.WorkersMin {
				t.Errorf("WorkersMin = %d, want %d", got.WorkersMin, tt.want.WorkersMin)
			}
			if got.WorkersMax != tt.want.WorkersMax {
				t.Errorf("WorkersMax = %d, want %d", got.WorkersMax, tt.want.WorkersMax)
			}
			if got.Locations != tt.want.Locations {
				t.Errorf("Locations = %q, want %q", got.Locations, tt.want.Locations)
			}
			if got.NetworkVolumeID != tt.want.NetworkVolumeID {
				t.Errorf("NetworkVolumeID = %q, want %q", got.NetworkVolumeID, tt.want.NetworkVolumeID)
			}
			if len(got.ModelReferences) != len(tt.want.ModelReferences) {
				t.Fatalf("ModelReferences len = %d, want %d (%#v)", len(got.ModelReferences), len(tt.want.ModelReferences), got.ModelReferences)
			}
			for i := range got.ModelReferences {
				if got.ModelReferences[i] != tt.want.ModelReferences[i] {
					t.Errorf("ModelReferences[%d] = %q, want %q", i, got.ModelReferences[i], tt.want.ModelReferences[i])
				}
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
