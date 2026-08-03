package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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

func TestOutputFlagRejectsUnsupportedFormat(t *testing.T) {
	root := GetRootCmd()
	if root.PersistentPreRunE == nil {
		t.Fatal("expected a root PersistentPreRunE to validate --output")
	}

	original := outputFormat
	t.Cleanup(func() { outputFormat = original })

	// `table` was never a real format but used to be accepted silently and return
	// json, which is how it ended up in the README.
	for _, bad := range []string{"table", "yml", "jsonl"} {
		outputFormat = bad
		err := root.PersistentPreRunE(root, nil)
		if err == nil {
			t.Errorf("--output=%s: expected an error, got nil", bad)
			continue
		}
		var ue *usageError
		if !errors.As(err, &ue) {
			t.Errorf("--output=%s: expected a *usageError (code usage_error), got %T", bad, err)
			continue
		}
		if ue.ErrorCode() != "usage_error" {
			t.Errorf("--output=%s: code = %q, want usage_error", bad, ue.ErrorCode())
		}
	}

	for _, ok := range []string{"json", "yaml", "YAML", " yaml "} {
		outputFormat = ok
		if err := root.PersistentPreRunE(root, nil); err != nil {
			t.Errorf("--output=%q: expected nil, got %v", ok, err)
		}
	}
}

// TestOutputValidationSurvivesShadowingHook covers the case a subcommand's own
// PersistentPreRun(E) used to bypass: without cobra.EnableTraverseRunHooks only
// the closest hook runs, so `exec` and `config` (both define one for their
// deprecation notice) reached their bodies with an unvalidated --output. For
// `config` that meant writing the config file and uploading an ssh key on an
// invocation that should have been rejected.
//
// The stand-in command below keeps the test off those real bodies; a regression
// makes it run instead of hanging on the network or mutating local state.
func TestOutputValidationSurvivesShadowingHook(t *testing.T) {
	if !cobra.EnableTraverseRunHooks {
		t.Fatal("cobra.EnableTraverseRunHooks must stay set, otherwise a subcommand that defines PersistentPreRun(E) shadows the root --output guard")
	}

	// This is the only test in the package that runs a *runnable* command, which
	// means it is the only one that reaches cobra.OnInitialize(initConfig) —
	// cobra's --help path returns before the initializers. initConfig writes
	// ~/.runpod/config.toml when it cannot read one, so without redirecting HOME
	// `go test` would create (or, on an unreadable config, overwrite and thereby
	// destroy the api key in) the real one. USERPROFILE covers windows, where
	// os.UserHomeDir reads that instead.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	root := GetRootCmd()
	t.Cleanup(func() {
		// pflag writes through to &outputFormat, so setting the flag restores the
		// var too — assigning outputFormat separately would be redundant. Reset
		// Changed as well, so a later test can't mistake this for a user-set flag.
		//nolint:errcheck // restoring the flag so later tests see the default
		root.PersistentFlags().Set("output", "json")
		root.PersistentFlags().Lookup("output").Changed = false
		root.SetArgs(nil)
		root.SetOut(nil)
		root.SetErr(nil)
	})

	var ownHookRan, bodyRan bool
	shadow := &cobra.Command{
		Use:    "shadow-hook-test",
		Hidden: true,
		// shadows the root hook, the way exec and config do
		PersistentPreRun: func(*cobra.Command, []string) { ownHookRan = true },
		RunE: func(*cobra.Command, []string) error {
			bodyRan = true
			return nil
		},
	}
	root.AddCommand(shadow)
	t.Cleanup(func() { root.RemoveCommand(shadow) })

	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)

	root.SetArgs([]string{"shadow-hook-test", "--output=table"})
	err := root.Execute()
	if bodyRan {
		t.Error("command body ran with --output=table; the root validation was skipped")
	}
	var ue *usageError
	if !errors.As(err, &ue) {
		t.Fatalf("expected a *usageError, got %T (%v)", err, err)
	}
	if ue.ErrorCode() != "usage_error" {
		t.Errorf("code = %q, want usage_error", ue.ErrorCode())
	}

	// traversing must add the root hook, not replace the subcommand's own — exec
	// and config print their deprecation notice from theirs.
	ownHookRan, bodyRan = false, false
	root.SetArgs([]string{"shadow-hook-test", "--output=yaml"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--output=yaml: expected nil, got %v", err)
	}
	if !ownHookRan {
		t.Error("the subcommand's own PersistentPreRun did not run")
	}
	if !bodyRan {
		t.Error("the subcommand body did not run")
	}

	// `help <cmd>` stays reachable with a bad --output. It is the one help path
	// that runs as a normal command, so it is the only one the guard could break;
	// `--help` and a bare parent return before the hooks on their own.
	root.SetArgs([]string{"help", "pod", "--output=table"})
	if err := root.Execute(); err != nil {
		t.Errorf("help with an invalid --output should still print help, got %v", err)
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
		// ValidateRequiredFlags bypasses SetFlagErrorFunc, so it must match here.
		`required flag(s) "image", "name" not set`,
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

// TestModelValidationMessagesAreNotUsageErrors closes a gap the cmd/model tests
// cannot see: they assert on output.Error directly, but Execute runs
// asUsageError FIRST, and any message beginning with a cobra usage prefix gets
// reclassified as usage_error and followed by a usage dump. A command author
// rewording a validation error into "invalid argument: …" would silently change
// its code and add ~1.3KB of usage text to stderr, with every cmd/model test
// still green.
func TestModelValidationMessagesAreNotUsageErrors(t *testing.T) {
	root := GetRootCmd()

	// the real messages returned by cmd/model's validation paths.
	for _, msg := range []string{
		`model-path "/x" does not exist`,
		`model-path "/x" must be a directory`,
		`model-path "/x" does not contain any files to upload`,
		"--wait-for-hash requires --model-path",
		"file-name is required when creating an upload",
		"file-size is required when creating an upload",
		"upload response missing upload session details",
		"upload response missing model version uuid required by --wait-for-hash",
		"unable to read model directory: stat /x: no such file or directory",
		"unable to set graphql timeout: bad duration",
		"upload completed but timed out waiting for the model hash; the model exists, do not re-upload: context deadline exceeded",
	} {
		if ue, ok := asUsageError(root, errors.New(msg)); ok {
			t.Errorf("%q was classified as a usage error (%v) — it would print a usage dump and report usage_error", msg, ue)
		}
	}
}
