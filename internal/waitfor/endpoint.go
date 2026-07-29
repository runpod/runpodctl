package waitfor

import (
	"context"
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
)

// EndpointHealthGetter is the slice of the rest client an endpoint wait needs.
type EndpointHealthGetter interface {
	GetEndpointHealth(endpointID string) (*api.EndpointHealth, error)
}

// EndpointWorkerPoller polls until at least one worker of endpointID can take a
// job (health reports it ready or already running).
//
// The endpoint's own worker list is not usable for this: `GET /endpoints/{id}
// ?includeWorkers=true` returns historical worker records, and against prod
// every entry on a warm, healthy endpoint read desiredStatus EXITED while
// /health reported five ready workers. Being listed is not being ready; the
// invoke service's health counts are the live signal.
func EndpointWorkerPoller(client EndpointHealthGetter, endpointID string) PollFunc {
	return func(ctx context.Context) (State, error) {
		health, err := client.GetEndpointHealth(endpointID)
		if err != nil {
			return State{}, err
		}
		workers := health.Workers
		detail := fmt.Sprintf("workers ready %d, initializing %d, throttled %d, unhealthy %d",
			workers.Ready, workers.Initializing, workers.Throttled, workers.Unhealthy)
		// a worker that is already running a job is usable too, so it counts.
		return State{Ready: workers.Ready > 0 || workers.Running > 0, Detail: detail}, nil
	}
}
