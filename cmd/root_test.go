package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
)

func TestRootCmd_Structure(t *testing.T) {
	root := GetRootCmd()

	if root.Use != "runpodctl" {
		t.Errorf("expected use 'runpodctl', got %s", root.Use)
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

func TestRootCmd_SilencesCobraOutput(t *testing.T) {
	root := GetRootCmd()
	if !root.SilenceUsage {
		t.Error("SilenceUsage should be true so runtime errors don't dump usage")
	}
	if !root.SilenceErrors {
		t.Error("SilenceErrors should be true so Cobra doesn't re-print errors")
	}
}

func TestAsUsageError(t *testing.T) {
	root := GetRootCmd()

	usageCases := []string{
		`unknown command "foo" for "runpodctl"`,
		"unknown flag: --bogus",
		"accepts 1 arg(s), received 0",
		"requires at least 1 arg(s), only received 0",
		`invalid argument "x" for "--count"`,
	}
	for _, msg := range usageCases {
		if _, ok := asUsageError(root, errors.New(msg)); !ok {
			t.Errorf("expected %q to be classified as a usage error", msg)
		}
	}

	runtimeCases := []string{
		"pod not found",
		"api request failed with status 500",
		"failed to create endpoint: server_error",
	}
	for _, msg := range runtimeCases {
		if _, ok := asUsageError(root, errors.New(msg)); ok {
			t.Errorf("expected %q NOT to be classified as a usage error", msg)
		}
	}

	// typed api/graphql errors must never be classified as usage errors, even
	// when the server message happens to start with a usage-ish word.
	if _, ok := asUsageError(root, &api.APIError{Message: "invalid argument: region", Status: 400}); ok {
		t.Error("a typed *api.APIError must not be classified as a usage error")
	}
	if _, ok := asUsageError(root, &api.GraphQLError{Message: "requires a valid gpu"}); ok {
		t.Error("a typed *api.GraphQLError must not be classified as a usage error")
	}

	// an already-wrapped usageError is recognized regardless of message.
	wrapped := &usageError{cmd: root, err: errors.New("some flag problem")}
	if _, ok := asUsageError(root, wrapped); !ok {
		t.Error("wrapped *usageError should be recognized")
	}
	if wrapped.ErrorCode() != "usage_error" {
		t.Errorf("usageError code = %q, want 'usage_error'", wrapped.ErrorCode())
	}
}

func TestRootCmd_HelpMentionsLegacy(t *testing.T) {
	root := GetRootCmd()

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})
	root.Execute()

	output := buf.String()
	if !strings.Contains(output, "deprecated") {
		t.Error("help should list deprecated commands")
	}
	if !strings.Contains(output, "project") {
		t.Error("help should mention legacy project command")
	}
	if !strings.Contains(output, "get models") {
		t.Error("help should mention legacy model command")
	}
}
