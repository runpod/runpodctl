package model

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/runpod/runpodctl/api"
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

func TestHandleModelRepoError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantHandled    bool
		wantStderrSub  string // empty = stderr must be empty
		wantStdoutSub  string // empty = stdout must be empty
	}{
		{
			name:        "nil error is a no-op",
			err:         nil,
			wantHandled: false,
		},
		{
			name:          "ErrModelRepoNotImplemented routes to stderr",
			err:           api.ErrModelRepoNotImplemented,
			wantHandled:   true,
			wantStderrSub: api.ErrModelRepoNotImplemented.Error(),
		},
		{
			name:          "feature-not-enabled message routes to stderr",
			err:           errors.New("Model Repo feature is not enabled for this user"),
			wantHandled:   true,
			wantStderrSub: "Model Repo feature is not enabled for this user",
		},
		{
			name:        "unrelated error is not handled",
			err:         errors.New("some other failure"),
			wantHandled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var handled bool
			stdout, stderr := captureStdStreams(t, func() {
				handled = handleModelRepoError(tt.err)
			})

			if handled != tt.wantHandled {
				t.Fatalf("handled = %v, want %v", handled, tt.wantHandled)
			}

			// CLAUDE.md: deprecation warnings / handled errors must go to stderr
			// only; stdout must stay clean for JSON-consuming agents.
			if stdout != "" {
				t.Fatalf("stdout must remain empty, got %q", stdout)
			}

			if tt.wantStderrSub == "" {
				if stderr != "" {
					t.Fatalf("expected empty stderr, got %q", stderr)
				}
				return
			}
			if !strings.Contains(stderr, tt.wantStderrSub) {
				t.Fatalf("stderr = %q, want substring %q", stderr, tt.wantStderrSub)
			}
		})
	}
}
