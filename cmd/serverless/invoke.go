package serverless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/runpod/runpodctl/internal/api"

	"github.com/spf13/viper"
)

// invokeClient is the slice of the invoke api the serverless commands use, so
// health/run/status can be tested without hitting the live service.
type invokeClient interface {
	EndpointHealth(ctx context.Context, endpointID string) (json.RawMessage, error)
	Run(ctx context.Context, endpointID string, input json.RawMessage) (*api.Job, error)
	JobStatus(ctx context.Context, endpointID, jobID string) (*api.Job, error)
}

var newInvokeClient = func() (invokeClient, error) {
	return api.NewInvokeClient()
}

// poll pacing for /status. Backoff keeps a long wait from hammering the api
// while still returning a fast job quickly. These are vars, not consts, so tests
// can run the loop without sleeping for real seconds.
var (
	// deliberately sub-second: every job is submitted on /run and then polled, so
	// this interval is the extra latency a fast job pays versus holding a
	// connection open on /runsync.
	pollInitialInterval = 500 * time.Millisecond
	pollMaxInterval     = 5 * time.Second
	// pollHeartbeat is how often a still-unchanged status is re-announced on
	// stderr, so a long wait does not look hung.
	pollHeartbeat = 30 * time.Second
)

// requestTimeout is the per-request deadline for a single invoke call (health,
// /run submit, one /status poll). It reuses the shared "timeout" config key that
// bounds every other api call, so there is one lever for "how long may a single
// api call take"; the separate --wait budget covers "how long may the job take".
func requestTimeout() time.Duration {
	timeout := viper.GetDuration("timeout")
	if timeout <= 0 {
		timeout = api.DefaultTimeout
	}
	return timeout
}

// minRequest is the floor for any single invoke call inside a wait. Clamping a
// request to the few milliseconds left of the budget guarantees a timeout instead
// of an answer, and would throw away a terminal status that was one round trip
// away — so a wait bound may overshoot by this much, and no more.
const minRequest = time.Second

// boundedRequestTimeout is the per-request deadline for a call made *inside* a
// wait: the shared per-call timeout, but never past the end of the wait budget.
// Without this the wait bound is a lie — a hung submit or first /status check
// would run for the full 30s per-call timeout even under --wait 1s.
//
// Only for calls inside a wait. A command with no wait budget (health,
// --no-wait, a plain status check) must use requestTimeout() directly: its
// deadline has already passed by this definition, and clamping it to the
// minRequest floor would give a single legitimate api call one second.
func boundedRequestTimeout(deadline time.Time) time.Duration {
	timeout := requestTimeout()
	if remaining := time.Until(deadline); remaining < timeout {
		timeout = remaining
	}
	if timeout < minRequest {
		timeout = minRequest
	}
	return timeout
}

// notef writes a progress note to stderr. stdout stays reserved for the json
// payload, so a caller piping stdout into a parser is unaffected.
func notef(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// waitForTerminal polls /status until the job is terminal or the deadline
// passes. It returns the last job it managed to fetch (never nil) so the caller
// can still print the payload it has, plus the error that stopped the wait.
func waitForTerminal(client invokeClient, endpointID string, job *api.Job, deadline time.Time) (*api.Job, error) {
	if job.IsTerminal() {
		return job, nil
	}
	if job.ID == "" {
		// nothing to poll: without an id the job is unreachable. That is the api
		// misbehaving, not a local mistake, so it gets an api code rather than the
		// cli_error fallback.
		return job, &api.APIError{
			Message: fmt.Sprintf("invoke api reported status %s without a job id, so the job cannot be polled", job.Status),
			Code:    "api_error",
			Status:  http.StatusOK,
		}
	}

	start := time.Now()
	interval := pollInitialInterval
	reportedStatus := job.Status
	lastReport := time.Now()
	notef("waiting for job %s: %s", job.ID, job.Status)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return job, waitTimeoutError(endpointID, job, time.Since(start))
		}

		sleep := interval
		if sleep > remaining {
			sleep = remaining
		}
		time.Sleep(sleep)
		if interval < pollMaxInterval {
			interval += interval / 2
			if interval > pollMaxInterval {
				interval = pollMaxInterval
			}
		}

		next, err := pollJobStatus(client, endpointID, job.ID, deadline)
		if err != nil {
			if !retryablePollError(err) {
				return job, fmt.Errorf("failed to get job status: %w", err)
			}
			// transient: a single 502 or dropped connection must not throw away a
			// job that is still running.
			if time.Until(deadline) <= 0 {
				// the budget is gone, so the actionable answer is the wait timeout
				// (which names the follow-up command), not the incidental transport
				// failure of the last attempt — that goes out as a note instead.
				notef("job %s: last status check failed (%v)", job.ID, err)
				return job, waitTimeoutError(endpointID, job, time.Since(start))
			}
			notef("job %s: status check failed (%v), retrying", job.ID, err)
			continue
		}

		job = next
		if job.IsTerminal() {
			notef("job %s: %s after %s", job.ID, job.Status, since(start))
			return job, nil
		}
		if job.Status != reportedStatus || time.Since(lastReport) >= pollHeartbeat {
			notef("waiting for job %s: %s (%s elapsed)", job.ID, job.Status, since(start))
			reportedStatus = job.Status
			lastReport = time.Now()
		}
	}
}

// pollJobStatus runs one /status call, bounded by both the per-request timeout
// and whatever is left of the wait budget (never below minRequest).
func pollJobStatus(client invokeClient, endpointID, jobID string, deadline time.Time) (*api.Job, error) {
	ctx, cancel := context.WithTimeout(context.Background(), boundedRequestTimeout(deadline))
	defer cancel()
	return client.JobStatus(ctx, endpointID, jobID)
}

// fetchJobStatus is the first /status call of a command.
//
// With a wait budget it applies the same transient-failure policy as the wait
// loop, because this is the call an agent makes after a 'timeout' error told it to
// poll — losing the job to a single 502 there would strand exactly the job the
// message was about. Without one it is a single call on the full per-call timeout,
// and any failure is reported as-is.
func fetchJobStatus(client invokeClient, endpointID, jobID string, deadline time.Time, waiting bool) (*api.Job, error) {
	if !waiting {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout())
		defer cancel()
		return client.JobStatus(ctx, endpointID, jobID)
	}

	for {
		job, err := pollJobStatus(client, endpointID, jobID, deadline)
		if err == nil {
			return job, nil
		}
		if !retryablePollError(err) || time.Until(deadline) <= 0 {
			return nil, err
		}
		notef("job %s: status check failed (%v), retrying", jobID, err)

		sleep := pollInitialInterval
		if remaining := time.Until(deadline); sleep > remaining {
			sleep = remaining
		}
		if sleep > 0 {
			time.Sleep(sleep)
		}
	}
}

// waitTimeoutError reports that the cli stopped waiting, not that the job broke.
// The message names the follow-up command, because the job is still running and
// the useful next step is to poll it rather than to re-invoke.
func waitTimeoutError(endpointID string, job *api.Job, waited time.Duration) error {
	return api.NewTimeoutError(
		"gave up waiting for job %s after %s (last status %s); the job is still running server-side, poll it with: runpodctl serverless status %s %s",
		job.ID, humanDuration(waited), job.Status, endpointID, job.ID,
	)
}

// retryablePollError reports whether a failed /status call is worth retrying
// while the wait budget lasts. Transport failures, cli-side request timeouts,
// rate limits and 5xx are transient; a 401/404 will never fix itself and must
// fail fast instead of burning the whole budget.
func retryablePollError(err error) bool {
	var timeoutErr *api.TimeoutError
	if errors.As(err, &timeoutErr) {
		return true
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= http.StatusInternalServerError
	}
	// transport-level failures arrive wrapped in *url.Error / *net.OpError.
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Op != "parse" {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

// jobOutcome maps the job a command ended up with to its exit behaviour. Only a
// terminal status other than COMPLETED is a failure of the command, even though
// the request itself succeeded. A job that is still queued or running is not:
// that is the expected answer for --wait 0 / --no-wait and for a plain 'status'
// check.
func jobOutcome(job *api.Job) error {
	// a 200 that carries neither an id nor a status is not a job at all — usually
	// a bare error object from the invoke api. Exiting 0 on it would report a
	// failed request as a success.
	if !job.HasEnvelope() {
		return api.NewNoJobEnvelopeError()
	}
	if job.Status == "" || job.Succeeded() || !job.IsTerminal() {
		return nil
	}
	return api.NewJobFailedError(job.ID, job.Status)
}

// humanDuration renders a duration for a progress note or error message,
// dropping sub-second noise — except below a second, where rounding to seconds
// would report a sub-second wait budget as "0s".
func humanDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}

func since(start time.Time) string { return humanDuration(time.Since(start)) }
