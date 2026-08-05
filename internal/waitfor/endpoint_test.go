package waitfor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
)

type fakeHealthClient struct {
	health *api.EndpointHealth
	err    error
	calls  int
}

func (f *fakeHealthClient) GetEndpointHealth(string) (*api.EndpointHealth, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.health, nil
}

func TestEndpointWorkerPoller(t *testing.T) {
	cases := []struct {
		name       string
		workers    api.EndpointHealthWorkers
		wantReady  bool
		wantDetail string
	}{
		{
			name:    "no workers at all",
			workers: api.EndpointHealthWorkers{},
			// running has to be in the detail: it is one of the two counters that
			// decides readiness, so hiding it makes the timeout error (and the
			// success line) impossible to attribute.
			wantDetail: "workers ready 0, running 0, initializing 0, throttled 0, unhealthy 0",
		},
		{
			name:       "a ready worker is ready",
			workers:    api.EndpointHealthWorkers{Idle: 1, Ready: 1},
			wantReady:  true,
			wantDetail: "workers ready 1, running 0",
		},
		{
			// a --workers-min worker stays RUNNING for its whole life and never
			// appears in the ready count, so running has to count as well.
			name:       "a running worker counts as ready",
			workers:    api.EndpointHealthWorkers{Running: 1},
			wantReady:  true,
			wantDetail: "workers ready 0, running 1",
		},
		{
			name:       "still pulling the image",
			workers:    api.EndpointHealthWorkers{Initializing: 1},
			wantDetail: "initializing 1",
		},
		{
			// no capacity in the gpu pool: never ready, and the count is the only
			// clue the user gets, so it has to reach the timeout error.
			name:       "throttled workers are not ready",
			workers:    api.EndpointHealthWorkers{Throttled: 2},
			wantDetail: "throttled 2",
		},
		{
			name:       "unhealthy workers are not ready",
			workers:    api.EndpointHealthWorkers{Unhealthy: 1},
			wantDetail: "unhealthy 1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeHealthClient{health: &api.EndpointHealth{Workers: tc.workers}}
			state, err := EndpointWorkerPoller(client, "endpoint-1")(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if state.Ready != tc.wantReady {
				t.Errorf("ready = %v, want %v", state.Ready, tc.wantReady)
			}
			if tc.wantDetail != "" && !strings.Contains(state.Detail, tc.wantDetail) {
				t.Errorf("detail = %q, want it to contain %q", state.Detail, tc.wantDetail)
			}
			if client.calls != 1 {
				t.Errorf("health calls = %d, want 1", client.calls)
			}
		})
	}
}

// The poller surfaces a health failure and lets Until decide whether it is worth
// retrying (it is: /health 404s an endpoint id it has not propagated yet).
func TestEndpointWorkerPollerReturnsHealthErrors(t *testing.T) {
	client := &fakeHealthClient{err: errors.New("endpoint not found")}
	_, err := EndpointWorkerPoller(client, "endpoint-1")(context.Background())
	if err == nil {
		t.Fatal("expected the health error to propagate")
	}
	if isFatalPollError(err) {
		t.Error("a health read failure must not be fatal to the wait")
	}
}

// A 404 *before* the endpoint has ever been readable is propagation lag and must
// be tolerated — that is exactly what the invoke service does right after a
// create.
func TestEndpointWorkerPollerTolerates404BeforeItEverAnswered(t *testing.T) {
	client := &fakeHealthClient{err: &api.APIError{Message: "endpoint not found", Status: 404}}
	_, err := EndpointWorkerPoller(client, "endpoint-1")(context.Background())
	if err == nil {
		t.Fatal("expected the health error to propagate")
	}
	if isFatalPollError(err) {
		t.Error("a 404 before the first successful read is propagation lag, not fatal")
	}
}

// ...but that tolerance is finite. An endpoint that never answers at all was
// deleted before it propagated, or never existed; waiting out the whole budget
// and then reporting wait_timeout tells an agent to retry a create that already
// happened.
func TestEndpointWorkerPollerGivesUpOnAnEndpointThatNeverAnswers(t *testing.T) {
	client := &fakeHealthClient{err: &api.APIError{Message: "endpoint not found", Status: 404}}
	poll := EndpointWorkerPoller(client, "endpoint-1")

	for i := 1; i < missesBeforeKnown; i++ {
		if _, err := poll(context.Background()); isFatalPollError(err) {
			t.Fatalf("poll %d of %d must still be treated as propagation lag, got a fatal %v", i, missesBeforeKnown, err)
		}
	}

	_, err := poll(context.Background())
	var fatal *FatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("expected a fatal error after %d misses, got %v", missesBeforeKnown, err)
	}
	if fatal.ErrorCode() != "not_found" {
		t.Errorf("code = %q, want not_found", fatal.ErrorCode())
	}
	// the message must not claim it was deleted *while waiting* (it was never
	// seen), and must not claim it never existed either (the id came from a create
	// that succeeded, and an agent told that would buy a second endpoint).
	if !strings.Contains(err.Error(), "deleted before it propagated") {
		t.Errorf("error = %q, want it to say the endpoint never propagated", err.Error())
	}
	if strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %q must not claim the endpoint never existed", err.Error())
	}
}

// A 500 in the middle of a run of 404s breaks the run: "consecutive" has to mean
// consecutive, or an endpoint that is merely flaky gets declared deleted.
func TestEndpointWorkerPollerResetsMissesOnANon404(t *testing.T) {
	client := &fakeHealthClient{health: &api.EndpointHealth{Workers: api.EndpointHealthWorkers{Initializing: 1}}}
	poll := EndpointWorkerPoller(client, "endpoint-1")
	if _, err := poll(context.Background()); err != nil {
		t.Fatalf("unexpected error on the first poll: %v", err)
	}

	client.err = &api.APIError{Message: "endpoint not found", Status: 404}
	if _, err := poll(context.Background()); isFatalPollError(err) {
		t.Fatalf("the first miss must be tolerated, got a fatal %v", err)
	}
	client.err = &api.APIError{Message: "internal server error", Status: 500}
	if _, err := poll(context.Background()); isFatalPollError(err) {
		t.Fatalf("a 5xx must never end the wait, got %v", err)
	}
	client.err = &api.APIError{Message: "endpoint not found", Status: 404}
	if _, err := poll(context.Background()); isFatalPollError(err) {
		t.Fatalf("this 404 is the first of a new run, so it must be tolerated, got a fatal %v", err)
	}
}

// An endpoint that the invoke service knew and then 404s twice was deleted out of
// band. Without a fatal case the wait would burn the whole budget, because a
// /health 404 is otherwise read as propagation lag.
func TestEndpointWorkerPollerFailsFastWhenTheEndpointDisappears(t *testing.T) {
	client := &fakeHealthClient{health: &api.EndpointHealth{Workers: api.EndpointHealthWorkers{Initializing: 1}}}
	poll := EndpointWorkerPoller(client, "endpoint-1")

	if _, err := poll(context.Background()); err != nil {
		t.Fatalf("unexpected error on the first poll: %v", err)
	}

	client.err = &api.APIError{Message: "endpoint not found", Status: 404}
	if _, err := poll(context.Background()); isFatalPollError(err) {
		t.Fatalf("the first miss must be tolerated, got a fatal %v", err)
	}

	_, err := poll(context.Background())
	var fatal *FatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("expected a fatal error once the endpoint disappeared twice, got %v", err)
	}
	if fatal.ErrorCode() != "not_found" {
		t.Errorf("code = %q, want not_found", fatal.ErrorCode())
	}
}

// A 500 after a successful read is not "deleted": it stays transient.
func TestEndpointWorkerPollerKeepsWaitingThroughA500(t *testing.T) {
	client := &fakeHealthClient{health: &api.EndpointHealth{Workers: api.EndpointHealthWorkers{Initializing: 1}}}
	poll := EndpointWorkerPoller(client, "endpoint-1")
	if _, err := poll(context.Background()); err != nil {
		t.Fatalf("unexpected error on the first poll: %v", err)
	}

	client.err = &api.APIError{Message: "internal server error", Status: 500}
	for i := 0; i < 3; i++ {
		if _, err := poll(context.Background()); isFatalPollError(err) {
			t.Fatalf("poll %d: a 5xx must never end the wait, got %v", i+1, err)
		}
	}
}
