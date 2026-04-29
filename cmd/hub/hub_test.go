package hub

import (
	"testing"
)

func TestHubCmd_Structure(t *testing.T) {
	if Cmd.Use != "hub" {
		t.Errorf("expected use 'hub', got %s", Cmd.Use)
	}

	expectedSubcommands := []string{"list", "search <term>", "get <id-or-owner/name>"}
	for _, expected := range expectedSubcommands {
		found := false
		for _, cmd := range Cmd.Commands() {
			if cmd.Use == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected subcommand %q not found", expected)
		}
	}
}
