package waitfor

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/runpod/runpodctl/internal/api"
)

// EndpointHealthGetter is the slice of the rest client an endpoint wait needs.
type EndpointHealthGetter interface {
	GetEndpointHealth(endpointID string) (*api.EndpointHealth, error)
}

// EndpointWorkerPoller polls until at least one worker of endpointID is ready or
// running.
//
// The endpoint's own worker list is not usable for this: `GET /endpoints/{id}
// ?includeWorkers=true` returns historical worker records, and against prod
// every entry on a warm, healthy endpoint read desiredStatus EXITED while
// /health reported five ready workers. Being listed is not being ready; the
// invoke service's health counts are the live signal.
//
// Both counters /health exposes are weaker than "a hot handler", and there is no
// third one to use:
//   - ready counts a flashboot-cached worker whose record reads EXITED
//     (ai-api/pkg/api/health.go counts() + pkg/workerstate.IsReady), so the first
//     request resumes it rather than reaching an already-running container.
//   - running counts desiredStatus RUNNING, which the control plane writes when
//     the worker row is created — i.e. when it is scheduled, before any container
//     exists (runpod-backend/model/src/pod/rentPod.ts).
//
// running still has to count: a --workers-min worker stays RUNNING for its whole
// life and never appears in ready, so dropping it would make that case wait
// forever. Both counters are therefore in Detail, so progress output and the
// timeout error show which one is about to fire.
func EndpointWorkerPoller(client EndpointHealthGetter, endpointID string) PollFunc {
	seen := false
	missed := 0
	return func(ctx context.Context) (State, error) {
		health, err := client.GetEndpointHealth(endpointID)
		if err != nil {
			if seen && isNotFoundStatus(err) {
				missed++
				if missed < missesBeforeGone {
					return State{Detail: "endpoint health unreadable in the last poll: " + err.Error(), Err: err}, nil
				}
				// the invoke service knew this endpoint and now does not: it was
				// deleted out of band. without this the wait burns the whole budget,
				// because a /health 404 is otherwise read as propagation lag.
				return State{}, &FatalError{
					Code: "not_found",
					Err:  fmt.Errorf("endpoint %s is no longer known to the invoke service, so it was deleted while waiting", endpointID),
				}
			}
			return State{}, err
		}
		seen = true
		missed = 0

		workers := health.Workers
		detail := fmt.Sprintf("workers ready %d, running %d, initializing %d, throttled %d, unhealthy %d",
			workers.Ready, workers.Running, workers.Initializing, workers.Throttled, workers.Unhealthy)
		return State{Ready: workers.Ready > 0 || workers.Running > 0, Detail: detail}, nil
	}
}

// isNotFoundStatus reports whether err came back as an http 404.
func isNotFoundStatus(err error) bool {
	var statuser interface{ HTTPStatus() int }
	return errors.As(err, &statuser) && statuser.HTTPStatus() == http.StatusNotFound
}
