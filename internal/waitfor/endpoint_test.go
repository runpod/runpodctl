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
			name:       "no workers at all",
			workers:    api.EndpointHealthWorkers{},
			wantDetail: "workers ready 0, initializing 0, throttled 0, unhealthy 0",
		},
		{
			name:      "a ready worker is ready",
			workers:   api.EndpointHealthWorkers{Idle: 1, Ready: 1},
			wantReady: true,
		},
		{
			// a worker that already picked up a job can serve requests too.
			name:      "a running worker counts as ready",
			workers:   api.EndpointHealthWorkers{Running: 1},
			wantReady: true,
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

func TestEndpointWorkerPollerReturnsHealthErrors(t *testing.T) {
	client := &fakeHealthClient{err: errors.New("not_found")}
	if _, err := EndpointWorkerPoller(client, "endpoint-1")(context.Background()); err == nil {
		t.Fatal("expected the health error to propagate")
	}
}
