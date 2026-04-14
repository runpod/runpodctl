package serverless

import (
	"bytes"
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
