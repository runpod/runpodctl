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

	expectedCommands := []string{"pod", "serverless", "template", "model", "network-volume", "registry", "user", "gpu", "datacenter", "billing"}
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

	expectedCommands := []string{"ssh", "doctor", "send <file>", "receive <code>", "version"}
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

func TestRootCmd_ProjectIsHidden(t *testing.T) {
	root := GetRootCmd()

	for _, cmd := range root.Commands() {
		if cmd.Use == "project" {
			if !cmd.Hidden {
				t.Error("project command should be hidden")
			}
			return
		}
	}
	t.Error("project command not found")
}

func TestRootCmd_HasLegacyCommands(t *testing.T) {
	root := GetRootCmd()

	// legacy commands should exist but be hidden
	legacyCommands := []string{"get", "create", "remove", "start", "stop", "config"}
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
	if flag.Usage != "output format (json, yaml)" {
		t.Errorf("expected usage 'output format (json, yaml)', got %s", flag.Usage)
	}
}

func TestRootCmd_HelpMentionsLegacy(t *testing.T) {
	root := GetRootCmd()

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})
	root.Execute()

	output := buf.String()
	if !strings.Contains(output, "legacy (deprecated):") {
		t.Error("help should list legacy commands")
	}
	if !strings.Contains(output, "project") {
		t.Error("help should mention legacy project command")
	}
	if !strings.Contains(output, "get models") {
		t.Error("help should mention legacy model command")
	}
}
