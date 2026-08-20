package registry

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRegistryCmd_Structure(t *testing.T) {
	if Cmd.Use != "registry" {
		t.Errorf("expected use 'registry', got %s", Cmd.Use)
	}

	// check alias is reg
	hasReg := false
	for _, alias := range Cmd.Aliases {
		if alias == "reg" {
			hasReg = true
		}
	}
	if !hasReg {
		t.Error("expected alias 'reg'")
	}

	// check subcommands - registry has no update
	expectedSubcommands := []string{"list", "get <registry-auth-id>", "create", "delete <registry-auth-id>"}
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

	// registry should NOT have update command
	for _, cmd := range Cmd.Commands() {
		if cmd.Use == "update" {
			t.Error("registry should not have update command")
		}
	}
}

func TestCreateCmd_RequiredFlags(t *testing.T) {
	flags := createCmd.Flags()

	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("username") == nil {
		t.Error("expected --username flag")
	}
	if flags.Lookup("password") == nil {
		t.Error("expected --password flag")
	}
	if flags.Lookup("password-stdin") == nil {
		t.Error("expected --password-stdin flag")
	}

	// --name and --username are enforced by cobra; --password deliberately is not,
	// because cobra checks required flags before RunE and would reject the
	// --password-stdin and prompt paths before they can supply a value.
	for flag, wantRequired := range map[string]bool{"name": true, "username": true, "password": false} {
		required := flags.Lookup(flag).Annotations[cobra.BashCompOneRequiredFlag] != nil
		if required != wantRequired {
			t.Errorf("--%s required = %v, want %v", flag, required, wantRequired)
		}
	}
}

// passing both would silently pick one; cobra must reject the combination.
func TestCreateCmd_PasswordFlagsAreMutuallyExclusive(t *testing.T) {
	cmd := createCmd
	groups := cmd.Flags().Lookup("password").Annotations["cobra_annotation_mutually_exclusive"]
	if len(groups) == 0 {
		t.Fatal("expected --password to be in a mutually exclusive group")
	}
	if !strings.Contains(groups[0], "password-stdin") {
		t.Errorf("expected the group to include password-stdin, got %v", groups)
	}
}
