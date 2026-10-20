package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestProjectRemoval(t *testing.T) {
	tempHome(t)
	t.Setenv("RUNPOD_API_KEY", "")
	root := GetRootCmd()
	t.Cleanup(func() {
		root.SetArgs(nil)
		root.SetOut(nil)
		root.SetErr(nil)
	})

	for _, args := range [][]string{
		{"project"},
		{"project", "new"},
		{"project", "start"},
		{"project", "deploy"},
		{"project", "build"},
		{"project", "create", "--name", "example", "--type", "Hello_World"},
		{"project", "dev", "--select-volume"},
		{"project", "start", "--prefix-pod-logs=false"},
		{"project", "--help"},
		{"project", "deploy", "--help"},
		{"project", "unknown", "--unknown-flag"},
		{"--output=yaml", "project", "build"},
		{"--output=invalid", "project", "deploy"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(args)
			if err := root.Execute(); !errors.Is(err, errProjectRemoved) {
				t.Fatalf("expected project removal error, got %v", err)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("command must leave error output to Execute: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}
