//go:build unix

package api

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestRetryWaitStopsOnSignal(t *testing.T) {
	if mode := os.Getenv("TEST_RETRY_SIGNAL_CHILD"); mode != "" {
		withRetryBackoff(t, 5*time.Second)
		outer := context.Background()
		if mode == "outer" {
			var stop context.CancelFunc
			outer, stop = signal.NotifyContext(outer, syscall.SIGTERM)
			defer stop()
		}
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		}()
		if waitToRetry(context.Background(), nil, 1) {
			t.Fatal("retry continued after signal")
		}
		select {
		case <-outer.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("the signal was consumed by the retry wait")
		}
		return
	}

	run := func(mode string) (time.Duration, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRetryWaitStopsOnSignal$")
		cmd.Env = append(os.Environ(), "TEST_RETRY_SIGNAL_CHILD="+mode)
		start := time.Now()
		out, err := cmd.CombinedOutput()
		if err != nil && mode == "outer" {
			t.Logf("child output: %s", out)
		}
		return time.Since(start), err
	}

	// no other handler: the re-raised signal ends the process.
	elapsed, err := run("default")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected the child to be killed by the signal, got %v", err)
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("expected death by SIGTERM, got %v", exitErr)
	}
	// the child's backoff is 5s; this bound includes process start (slow under
	// -race), so it only has to show the wait was cut short.
	if elapsed > 3*time.Second {
		t.Fatalf("signal took %v to stop retry wait", elapsed)
	}

	// an outer handler (e.g. a --wait loop) still receives it.
	if _, err := run("outer"); err != nil {
		t.Fatalf("outer handler did not see the signal: %v", err)
	}
}
