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
