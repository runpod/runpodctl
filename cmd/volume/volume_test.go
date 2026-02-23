package volume

import (
	"testing"
)

func TestVolumeCmd_Structure(t *testing.T) {
	if Cmd.Use != "network-volume" {
		t.Errorf("expected use 'network-volume', got %s", Cmd.Use)
	}

	// check aliases
	hasNv := false
	for _, alias := range Cmd.Aliases {
		if alias == "nv" {
			hasNv = true
		}
	}
	if !hasNv {
		t.Error("expected alias 'nv'")
	}

	// check subcommands
	expectedSubcommands := []string{"list", "get <volume-id>", "create", "update <volume-id>", "delete <volume-id>"}
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

func TestCreateCmd_RequiredFlags(t *testing.T) {
	flags := createCmd.Flags()

	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("size") == nil {
		t.Error("expected --size flag")
	}
	if flags.Lookup("data-center-id") == nil {
		t.Error("expected --data-center-id flag")
	}
}
