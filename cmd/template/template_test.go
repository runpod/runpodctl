package template

import (
	"testing"
)

func TestTemplateCmd_Structure(t *testing.T) {
	if Cmd.Use != "template" {
		t.Errorf("expected use 'template', got %s", Cmd.Use)
	}

	// check aliases
	hasTpl := false
	hasTemplates := false
	for _, alias := range Cmd.Aliases {
		if alias == "tpl" {
			hasTpl = true
		}
		if alias == "templates" {
			hasTemplates = true
		}
	}
	if !hasTpl {
		t.Error("expected alias 'tpl'")
	}
	if !hasTemplates {
		t.Error("expected alias 'templates'")
	}

	// check subcommands
	expectedSubcommands := []string{"list", "get <template-id>", "create", "update <template-id>", "delete <template-id>"}
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

	// check required flags exist
	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("image") == nil {
		t.Error("expected --image flag")
	}
	if flags.Lookup("serverless") == nil {
		t.Error("expected --serverless flag")
	}
}
