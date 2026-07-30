// Package waitfor blocks until a freshly created resource is usable rather than
// merely scheduled.
//
// The runpod control plane answers a create call as soon as the resource is
// scheduled: a pod reports desiredStatus RUNNING before its image is pulled and
// long before sshd accepts a connection. Callers that want "usable" have to
// poll, so this package owns that loop once: a caller supplies a PollFunc that
// answers "ready yet?" and gets bounded waiting, non-spammy progress on a
// caller-provided writer (never stdout — stdout is the data channel), and a
// typed error carrying the resource id and the last known state when the wait
// does not finish.
package waitfor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Defaults for Options. The poll interval is deliberately not the 1s used by
// the legacy project ssh loop: a 10 minute wait at 1s is 600 api calls per
// created pod, and no readiness signal here changes that fast.
const (
	DefaultTimeout       = 10 * time.Minute
	DefaultInterval      = 5 * time.Second
	DefaultProgressEvery = 15 * time.Second
)

// Error codes emitted by a wait. Both are part of the cli's stable error
// vocabulary (see internal/api/client.go and README's error format table).
const (
	// CodeTimeout means the wait budget ran out. The resource still exists.
	CodeTimeout = "wait_timeout"
	// CodeInterrupted means the wait was cancelled (ctrl-c / SIGTERM). The
	// resource still exists.
	CodeInterrupted = "wait_interrupted"
)

// State is the outcome of a single poll.
type State struct {
	// Ready reports whether the resource is usable now.
	Ready bool
	// Detail is a short lowercase note about the current state. It is shown in
	// progress output and, crucially, in the timeout error: "the last known
	// state" is the only thing an agent has to debug a wait that did not finish.
	Detail string
	// Err is the poll error behind Detail, when the state came from a tolerated
	// failure rather than from a successful read. Callers that need to preserve an
	// error chain across the wait (errors.Is/As on the underlying failure) read it
	// from the returned State.
	Err error
}

// PollFunc answers whether a resource is usable yet.
//
// A poll that simply cannot answer yet (resource not visible, port not
// allocated, no worker started) should return a not-ready State with a Detail
// explaining why. A returned error is treated as "not ready, and here is why"
// unless it is fatal (see FatalError and Until): a wait must survive a transient
// api failure, because the resource it is waiting on already exists and bills.
type PollFunc func(ctx context.Context) (State, error)

// FatalError marks a poll failure that will never resolve, so Until stops
// immediately instead of retrying until the deadline. Use it for a resource that
// cannot become ready (a pod that has exited, one that no longer exists), not
// for an api that happened to answer badly.
type FatalError struct {
	// Code is the stable machine-readable code to emit. It must come from the
	// existing vocabulary in internal/api/client.go.
	Code string
	Err  error
}

// Error implements the error interface.
func (e *FatalError) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying error.
func (e *FatalError) Unwrap() error { return e.Err }

// ErrorCode returns the stable code for the failure.
func (e *FatalError) ErrorCode() string { return e.Code }

// fatalPollCodes are the error codes that no amount of polling fixes: the
// request will be rejected identically every time. Everything else (network
// failures, 5xx, rate limits, graphql hiccups, and the invoke service 404ing an
// endpoint id it has not learned yet) is transient by default and must not end a
// wait — the resource exists and is billing, so giving up early is the expensive
// answer, and reporting it with a transport code would tell an agent to retry
// the create and buy a second one.
var fatalPollCodes = map[string]bool{
	"unauthorized":   true,
	"forbidden":      true,
	"no_credentials": true,
	"bad_request":    true,
}

// fatalPollStatuses are the http statuses that mean the *request* is wrong, so
// every retry is answered identically.
//
// Codes alone are not enough: the pod wait's only api call is graphql GetPods(),
// and every graphql failure is an *api.GraphQLError whose ErrorCode() is the
// constant "graphql_error" — so none of fatalPollCodes above is reachable on
// that path. Prod graphql answers a bad key with http 401 (probed), which
// without this check would burn the entire wait budget while the pod bills and
// then report wait_timeout instead of the auth failure. 404/429/5xx stay
// transient on purpose; so does a graphql 200 whose body carries an errors
// array, which has no status to judge.
var fatalPollStatuses = map[int]bool{
	http.StatusBadRequest:   true,
	http.StatusUnauthorized: true,
	http.StatusForbidden:    true,
}

// isFatalPollError reports whether err should end the wait immediately.
func isFatalPollError(err error) bool {
	var fatal *FatalError
	if errors.As(err, &fatal) {
		return true
	}
	var coder interface{ ErrorCode() string }
	if errors.As(err, &coder) && fatalPollCodes[coder.ErrorCode()] {
		return true
	}
	var statuser interface{ HTTPStatus() int }
	if errors.As(err, &statuser) && fatalPollStatuses[statuser.HTTPStatus()] {
		return true
	}
	return false
}

// Options configures Until. The zero value is usable: every field falls back to
// a default, and a nil Progress silences progress output.
type Options struct {
	// Label names what is being waited for, including the resource id, e.g.
	// "ssh on pod abc123". It appears in progress lines and in the error.
	Label string
	// Timeout is the total wait budget (DefaultTimeout when <= 0).
	Timeout time.Duration
	// Interval is the delay between polls (DefaultInterval when <= 0).
	Interval time.Duration
	// ProgressEvery throttles progress lines (DefaultProgressEvery when <= 0).
	ProgressEvery time.Duration
	// Progress receives progress lines. Must never be os.Stdout: stdout stays a
	// single clean json payload so scripts parse exactly one object. nil is fine.
	Progress io.Writer

	// Now and Sleep are injection points for tests, so the wait-loop tests run
	// instantly instead of sleeping for minutes.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// Error is returned when a wait does not finish successfully. It carries a
// stable code plus the label (which includes the resource id) and the last known
// state, so an agent that gave up waiting still knows what it created and why it
// stopped.
type Error struct {
	Code    string
	Label   string
	Last    string
	Elapsed time.Duration
}

// Error implements the error interface.
func (e *Error) Error() string {
	verb := "timed out"
	if e.Code == CodeInterrupted {
		verb = "interrupted"
	}
	msg := fmt.Sprintf("%s after %s waiting for %s", verb, e.Elapsed.Round(time.Second), e.Label)
	if e.Last != "" {
		msg += fmt.Sprintf("; last known state: %s", e.Last)
	}
	return msg
}

// ErrorCode returns the stable machine-readable code for the failed wait.
func (e *Error) ErrorCode() string { return e.Code }

// Until polls until the resource is ready, the timeout expires, or ctx is
// cancelled. It returns the last observed State either way, so callers can
// report progress they made even on failure.
//
// Cancellation is observed between polls: an in-flight poll is not aborted, so a
// ctrl-c is honoured within roughly one poll's latency.
func Until(ctx context.Context, poll PollFunc, opts Options) (State, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	progressEvery := opts.ProgressEvery
	if progressEvery <= 0 {
		progressEvery = DefaultProgressEvery
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}

	start := now()
	deadline := start.Add(timeout)
	lastProgress := start

	printf := func(format string, args ...interface{}) {
		if opts.Progress == nil {
			return
		}
		fmt.Fprintf(opts.Progress, format+"\n", args...) //nolint:errcheck
	}

	printf("waiting for %s (timeout %s)", opts.Label, timeout)

	var last State
	for {
		state, err := poll(ctx)
		if err != nil {
			if isFatalPollError(err) {
				// keep the underlying code (unauthorized, conflict, ...) and still
				// name the resource, so the id is never lost.
				return last, fmt.Errorf("waiting for %s: %w", opts.Label, err)
			}
			// a transient failure is just an unknown state: keep waiting, and carry
			// the reason into progress and into the timeout error.
			state = State{Detail: err.Error(), Err: err}
		}
		last = state
		elapsed := now().Sub(start)

		if state.Ready {
			// the detail goes on the success line too, not only the failure ones: a
			// readiness predicate can be satisfied by more than one signal (the
			// endpoint poll accepts ready *or* running workers), and without it
			// neither an operator nor a test can tell which one fired.
			printf("%s ready after %s: %s", opts.Label, elapsed.Round(time.Second), detailOr(state.Detail))
			return state, nil
		}

		if now().Sub(lastProgress) >= progressEvery {
			lastProgress = now()
			printf("still waiting for %s (%s elapsed): %s", opts.Label, elapsed.Round(time.Second), detailOr(state.Detail))
		}

		remaining := deadline.Sub(now())
		if remaining <= 0 {
			return last, &Error{Code: CodeTimeout, Label: opts.Label, Last: detailOr(state.Detail), Elapsed: now().Sub(start)}
		}
		wait := interval
		if wait > remaining {
			wait = remaining
		}
		if err := sleep(ctx, wait); err != nil {
			return last, &Error{Code: CodeInterrupted, Label: opts.Label, Last: detailOr(state.Detail), Elapsed: now().Sub(start)}
		}
	}
}

func detailOr(detail string) string {
	if detail == "" {
		return "not ready"
	}
	return detail
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
