package pod

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func TestPodCmd_Structure(t *testing.T) {
	if Cmd.Use != "pod" {
		t.Errorf("expected use 'pod', got %s", Cmd.Use)
	}

	// check aliases
	found := false
	for _, alias := range Cmd.Aliases {
		if alias == "pods" {
			found = true
		}
	}
	if !found {
		t.Error("expected alias 'pods'")
	}

	// check subcommands exist
	expectedSubcommands := []string{"list", "get <pod-id>", "create", "update <pod-id>", "start <pod-id>", "stop <pod-id>", "restart <pod-id>", "reset <pod-id>", "delete <pod-id>"}
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

	if flags.Lookup("compute-type") == nil {
		t.Error("expected --compute-type flag")
	}
	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("include-machine") == nil {
		t.Error("expected --include-machine flag")
	}
	if flags.Lookup("include-network-volume") == nil {
		t.Error("expected --include-network-volume flag")
	}
}

func TestCreateCmd_Flags(t *testing.T) {
	flags := createCmd.Flags()

	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("image") == nil {
		t.Error("expected --image flag")
	}
	if flags.Lookup("gpu-type-ids") == nil {
		t.Error("expected --gpu-type-ids flag")
	}
	if flags.Lookup("gpu-count") == nil {
		t.Error("expected --gpu-count flag")
	}
	if flags.Lookup("volume-in-gb") == nil {
		t.Error("expected --volume-in-gb flag")
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

func TestPodCmd_Help(t *testing.T) {
	output, err := executeCommand(Cmd, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output == "" {
		t.Error("expected help output")
	}
}
