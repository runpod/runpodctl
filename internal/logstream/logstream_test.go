package logstream

import (
	"errors"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/spf13/cobra"
)

// newTestCommand builds a command carrying the shared flags, so Resolve sees the
// same cobra state the real commands give it (Changed("tail") in particular).
func newTestCommand(args ...string) (*cobra.Command, *Flags) {
	flags := &Flags{}
	cmd := &cobra.Command{Use: "logs", RunE: func(*cobra.Command, []string) error { return nil }}
	flags.Register(cmd)
	cmd.SetArgs(args)
	_ = cmd.Flags().Parse(args)
	return cmd, flags
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var coder interface{ ErrorCode() string }
	if !errors.As(err, &coder) {
		t.Fatalf("err %v carries no ErrorCode()", err)
	}
	return coder.ErrorCode()
}

// The default has to be the api's own default, so `pod logs <id>` behaves the
// same as an unflagged request.
func TestResolveDefaultsToTail100(t *testing.T) {
	cmd, flags := newTestCommand()

	opts, err := flags.Resolve(cmd, time.Now())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opts.Tail == nil || *opts.Tail != 100 {
		t.Errorf("tail = %v, want 100", opts.Tail)
	}
	if opts.Source != "" {
		t.Errorf("source = %q, want empty (both)", opts.Source)
	}
}

// tail=0 means "live only" and must reach the wire as an explicit 0, not be
// mistaken for unset and replaced by the api default of 100.
func TestResolveKeepsExplicitTailZero(t *testing.T) {
	cmd, flags := newTestCommand("--tail", "0")

	opts, err := flags.Resolve(cmd, time.Now())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// the query rendering of this is asserted in the api package, which owns it.
	if opts.Tail == nil || *opts.Tail != 0 {
		t.Fatalf("tail = %v, want an explicit 0", opts.Tail)
	}
}

// The api answers an oversized tail with a bare 422, which names neither the flag
// nor the limit.
func TestResolveRejectsOutOfRangeTail(t *testing.T) {
	for _, value := range []string{"-1", "5001"} {
		cmd, flags := newTestCommand("--tail", value)

		_, err := flags.Resolve(cmd, time.Now())
		if err == nil {
			t.Fatalf("--tail %s was accepted", value)
		}
		if code := codeOf(t, err); code != "usage_error" {
			t.Errorf("--tail %s code = %q, want usage_error", value, code)
		}
	}
}

func TestResolveRejectsUnknownSource(t *testing.T) {
	cmd, flags := newTestCommand("--source", "stdout")

	_, err := flags.Resolve(cmd, time.Now())
	if err == nil {
		t.Fatal("expected --source stdout to be rejected")
	}
	if code := codeOf(t, err); code != "usage_error" {
		t.Errorf("code = %q, want usage_error", code)
	}
}

// "both" is not a wire value: the api returns both sources when the param is
// absent, and sending source=both would be a 422.
func TestResolveOmitsSourceForBoth(t *testing.T) {
	for _, value := range []string{"both", ""} {
		cmd, flags := newTestCommand("--source", value)

		opts, err := flags.Resolve(cmd, time.Now())
		if err != nil {
			t.Fatalf("resolve %q: %v", value, err)
		}
		if opts.Source != "" {
			t.Errorf("source %q produced param %q, want it omitted", value, opts.Source)
		}
	}
}

func TestResolvePassesThroughRealSources(t *testing.T) {
	for _, value := range []string{"container", "system"} {
		cmd, flags := newTestCommand("--source", value)

		opts, err := flags.Resolve(cmd, time.Now())
		if err != nil {
			t.Fatalf("resolve %q: %v", value, err)
		}
		if opts.Source != value {
			t.Errorf("source = %q, want %q", opts.Source, value)
		}
	}
}

// The ask behind this flag was "the last 30 minutes", so a duration has to work
// without the user computing a timestamp.
func TestResolveConvertsRelativeSince(t *testing.T) {
	now := time.Date(2026, 8, 13, 18, 30, 0, 0, time.UTC)
	cmd, flags := newTestCommand("--since", "30m")

	opts, err := flags.Resolve(cmd, now)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opts.Since != "2026-08-13T18:00:00Z" {
		t.Errorf("since = %q, want 2026-08-13T18:00:00Z", opts.Since)
	}
	// since wins server-side, so tail must not also be sent.
	if opts.Tail != nil {
		t.Errorf("tail = %v, want nil when since is set", *opts.Tail)
	}
}

func TestResolveAcceptsAbsoluteSince(t *testing.T) {
	cmd, flags := newTestCommand("--since", "2026-08-13T18:00:00Z")

	opts, err := flags.Resolve(cmd, time.Now())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opts.Since != "2026-08-13T18:00:00Z" {
		t.Errorf("since = %q", opts.Since)
	}
}

// A non-utc timestamp has to be normalized, since the api parses rfc3339 but the
// cursor semantics are easier to reason about in utc.
func TestResolveNormalizesSinceToUTC(t *testing.T) {
	cmd, flags := newTestCommand("--since", "2026-08-13T20:00:00+02:00")

	opts, err := flags.Resolve(cmd, time.Now())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opts.Since != "2026-08-13T18:00:00Z" {
		t.Errorf("since = %q, want the utc equivalent 2026-08-13T18:00:00Z", opts.Since)
	}
}

func TestResolveRejectsMalformedSince(t *testing.T) {
	for _, value := range []string{"yesterday", "notatimestamp", "0m", "-5m"} {
		cmd, flags := newTestCommand("--since", value)

		_, err := flags.Resolve(cmd, time.Now())
		if err == nil {
			t.Fatalf("--since %q was accepted", value)
		}
		if code := codeOf(t, err); code != "usage_error" {
			t.Errorf("--since %q code = %q, want usage_error", value, code)
		}
	}
}

func TestResolveRejectsNonPositiveMaxWait(t *testing.T) {
	cmd, flags := newTestCommand("--max-wait", "0s")

	_, err := flags.Resolve(cmd, time.Now())
	if err == nil {
		t.Fatal("expected --max-wait 0s to be rejected")
	}
	if code := codeOf(t, err); code != "usage_error" {
		t.Errorf("code = %q, want usage_error", code)
	}
}

func TestResolveDefaultMaxWait(t *testing.T) {
	cmd, flags := newTestCommand()

	if _, err := flags.Resolve(cmd, time.Now()); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if flags.MaxWait != DefaultMaxWait {
		t.Errorf("max-wait = %s, want %s", flags.MaxWait, DefaultMaxWait)
	}
}

func TestCombineStreamErrorsPartialFailureSucceeds(t *testing.T) {
	targets := []Target{{WorkerID: "w1"}, {WorkerID: "w2"}}
	errs := []error{errors.New("w1 vanished"), nil}

	// one worker of two dying is churn, not a failed command: the other worker's
	// output is still what was asked for.
	if err := combineStreamErrors(targets, errs); err != nil {
		t.Errorf("err = %v, want nil when at least one stream worked", err)
	}
}

func TestCombineStreamErrorsTotalFailureReturnsFirst(t *testing.T) {
	targets := []Target{{WorkerID: "w1"}, {WorkerID: "w2"}}
	first := &api.APIError{Message: "unauthorized", Status: 401}
	errs := []error{first, errors.New("second")}

	err := combineStreamErrors(targets, errs)
	if !errors.Is(err, error(first)) {
		t.Fatalf("err = %v, want the first error preserved", err)
	}
	// the code has to survive so an agent still sees `unauthorized` rather than a
	// generic cli_error.
	if code := codeOf(t, err); code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", code)
	}
}

func TestCombineStreamErrorsSingleTargetReturnsError(t *testing.T) {
	targets := []Target{{}}
	sentinel := errors.New("boom")

	if err := combineStreamErrors(targets, []error{sentinel}); !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v", err, sentinel)
	}
}
