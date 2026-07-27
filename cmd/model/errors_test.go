package model

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/runpod/runpodctl/api"
	"github.com/spf13/cobra"
)

// captureStdStreams runs fn with os.Stdout and os.Stderr replaced by pipes and
// returns whatever each stream received. It exists to assert that informational
// and error output goes to the correct stream — a regression class CLAUDE.md
// explicitly calls out (legacy commands losing stderr ⇒ corrupts stdout for
// JSON-consuming agents).
func captureStdStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	origStdout, origStderr := os.Stdout, os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe stdout: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe stderr: %v", err)
	}
	os.Stdout, os.Stderr = stdoutW, stderrW

	var (
		wg                   sync.WaitGroup
		stdoutBuf, stderrBuf strings.Builder
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stdoutBuf, stdoutR)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stderrBuf, stderrR)
	}()

	defer func() {
		os.Stdout, os.Stderr = origStdout, origStderr
	}()

	fn()

	_ = stdoutW.Close()
	_ = stderrW.Close()
	wg.Wait()
	_ = stdoutR.Close()
	_ = stderrR.Close()

	return stdoutBuf.String(), stderrBuf.String()
}

func TestModelRepoError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{
			name:    "nil error is a no-op",
			err:     nil,
			wantErr: false,
		},
		{
			name:    "ErrModelRepoNotImplemented is recognized",
			err:     api.ErrModelRepoNotImplemented,
			wantErr: true,
		},
		{
			name:    "feature-not-enabled message is recognized",
			err:     errors.New("Model Repo feature is not enabled for this user"),
			wantErr: true,
		},
		{
			name:    "unrelated error is not recognized",
			err:     errors.New("some other failure"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got error
			// modelRepoError must not print: the handler returns the error and
			// the Execute sink emits the single JSON object. Printing here as
			// well would double-print (the bug CON-683 removed elsewhere), and
			// swallowing it made the process exit 0 on a real failure.
			stdout, stderr := captureStdStreams(t, func() {
				got = modelRepoError(tt.err)
			})

			if (got != nil) != tt.wantErr {
				t.Fatalf("modelRepoError() = %v, wantErr %v", got, tt.wantErr)
			}
			if stdout != "" {
				t.Fatalf("stdout must remain empty, got %q", stdout)
			}
			if stderr != "" {
				t.Fatalf("stderr must remain empty (the sink prints), got %q", stderr)
			}
		})
	}
}

// TestModelCommandsUseRunE pins the exit-code contract: a model command that
// fails must return its error so cobra exits non-zero. They previously used
// Run: with a swallowed error, so a real Model Repo failure exited 0 and any
// script doing `runpodctl model add … && echo ok` printed ok.
func TestModelCommandsUseRunE(t *testing.T) {
	for _, c := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{"model list", listCmd},
		{"model list (legacy alias)", GetModelsCmd},
		{"model add", addCmd},
		{"model add (legacy alias)", AddModelToRepoCmd},
		{"model remove", removeCmd},
		{"model remove (legacy alias)", RemoveModelCmd},
	} {
		if c.cmd == nil {
			t.Errorf("%s: command is nil — the test can no longer see it", c.name)
			continue
		}
		if c.cmd.RunE == nil {
			t.Errorf("%s: RunE is nil — errors cannot propagate to the exit code", c.name)
		}
		if c.cmd.Run != nil {
			t.Errorf("%s: Run is set; use RunE so errors reach the sink", c.name)
		}
	}
}
