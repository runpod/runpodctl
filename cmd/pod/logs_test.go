package pod

import (
	"strings"
	"testing"
)

func TestLogsCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range Cmd.Commands() {
		if cmd.Use == "logs <pod-id>" {
			found = true
		}
	}
	if !found {
		t.Error("pod logs is not registered on the pod command")
	}
}

// The shared flags have to actually reach this command; a missing Register call
// would only show up as a cobra "unknown flag" at runtime.
func TestLogsCmdHasSharedFlags(t *testing.T) {
	for _, name := range []string{"tail", "since", "source", "follow", "max-wait"} {
		if logsCmd.Flags().Lookup(name) == nil {
			t.Errorf("--%s is not registered", name)
		}
	}
	if logsCmd.Flags().ShorthandLookup("f") == nil {
		t.Error("-f shorthand for --follow is missing")
	}
}

// Help text is the only place a user learns these two non-obvious behaviors, and
// both cost real debugging time when unknown: that a stopped pod still has logs,
// and that system lines are where a failed deploy explains itself.
func TestLogsCmdHelpExplainsSources(t *testing.T) {
	help := logsCmd.Long + logsCmd.Example
	for _, want := range []string{"container", "system", "json lines"} {
		if !strings.Contains(help, want) {
			t.Errorf("help text does not mention %q", want)
		}
	}
}

// Repo constraint: all cli text is lowercase.
func TestLogsCmdTextIsLowercase(t *testing.T) {
	if logsCmd.Short != strings.ToLower(logsCmd.Short) {
		t.Errorf("short description is not lowercase: %q", logsCmd.Short)
	}
}
