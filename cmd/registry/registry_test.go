package registry

import (
	"testing"
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
}
