package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCmd_Structure(t *testing.T) {
	root := GetRootCmd()

	if root.Use != "runpod" {
		t.Errorf("expected use 'runpod', got %s", root.Use)
	}
}

func TestRootCmd_HasResourceCommands(t *testing.T) {
	root := GetRootCmd()

	expectedCommands := []string{"pod", "serverless", "template", "volume", "registry"}
	for _, expected := range expectedCommands {
		found := false
		for _, cmd := range root.Commands() {
			if cmd.Use == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected command %s not found", expected)
		}
	}
}

func TestRootCmd_HasUtilityCommands(t *testing.T) {
	root := GetRootCmd()

	expectedCommands := []string{"ssh", "config", "send <file>", "receive <code>", "project", "version"}
	for _, expected := range expectedCommands {
		found := false
		for _, cmd := range root.Commands() {
			if cmd.Use == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected command %s not found", expected)
		}
	}
}

func TestRootCmd_HasLegacyCommands(t *testing.T) {
	root := GetRootCmd()

	// legacy commands should exist but be hidden
	legacyCommands := []string{"get", "create", "remove", "start", "stop"}
	for _, expected := range legacyCommands {
		found := false
		for _, cmd := range root.Commands() {
			if cmd.Use == expected {
				found = true
				if !cmd.Hidden {
					t.Errorf("legacy command %s should be hidden", expected)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected legacy command %s not found", expected)
		}
	}
}

func TestRootCmd_OutputFlag(t *testing.T) {
	root := GetRootCmd()

	flag := root.PersistentFlags().Lookup("output")
	if flag == nil {
		t.Error("expected --output flag")
	}
	if flag.Shorthand != "o" {
		t.Errorf("expected shorthand 'o', got %s", flag.Shorthand)
	}
	if flag.DefValue != "json" {
		t.Errorf("expected default 'json', got %s", flag.DefValue)
	}
}

func TestRootCmd_HelpMentionsLegacy(t *testing.T) {
	root := GetRootCmd()

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})
	root.Execute()

	output := buf.String()
	if !strings.Contains(output, "legacy commands") {
		t.Error("help should mention legacy commands")
	}
}
