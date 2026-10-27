package waitfor

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/podstate"
	"github.com/runpod/runpodctl/internal/sshconnect"
)

// PodLister is the slice of the api client a pod wait needs: the pod list with
// its runtime ports, which ssh readiness is read from.
type PodLister interface {
	GetPods() ([]*api.LegacyPod, error)
}

// terminalPodStatuses are the desiredStatus values a pod never comes back from.
// A pod in one of these will not become reachable however long you wait, so the
// wait ends immediately rather than billing out the full budget.
var terminalPodStatuses = map[string]bool{
	"EXITED":     true,
	"TERMINATED": true,
	"DEAD":       true,
	"ERROR":      true, // rest v2: the container is in an unrecoverable state
}

// PodReader is optionally implemented by a PodLister that can read one pod by
// id. The v2 list can omit runtime that the single-pod read reports, and
// runtime is what both waits key on, so a listed pod without it is re-read.
type PodReader interface {
	GetPodRuntime(podID string) (*api.LegacyPod, error)
}

// podFinder holds the cross-poll bookkeeping (whether the pod has ever been
// seen, and the run of consecutive misses) shared by every pod poll, so the
// not-found and terminal-status logic lives in exactly one place.
type podFinder struct {
	lister PodLister
	podID  string
	seen   bool
	missed int
}

// locate does one list+find with the shared miss/terminal accounting. It returns
// the listed, non-terminal pod (found == true) in any non-terminal desired state,
// including ones that are not running yet (e.g. created); each poller still does
// its own readiness check on the result. When found == false the State and error
// are what the poller must return verbatim: the error is either a transient read
// error or a *FatalError that ends the wait.
func (f *podFinder) locate() (pod *api.LegacyPod, st State, found bool, err error) {
	pods, listErr := f.lister.GetPods()
	if listErr != nil {
		// a failed read is an unknown state, not an absence, and it breaks the
		// run of consecutive misses -- otherwise a short list, a graphql blip and
		// a second short list would add up to a not_found for a live pod.
		f.missed = 0
		return nil, State{}, false, listErr
	}

	for _, candidate := range pods {
		if candidate != nil && candidate.ID == f.podID {
			pod = candidate
			break
		}
	}
	if pod == nil {
		if f.seen {
			f.missed++
			if f.missed < missesBeforeGone {
				// one short list is an unknown state, not a verdict: every other
				// anomaly here (5xx, malformed json, a network blip) is tolerated to
				// the deadline because the pod exists and bills, and declaring it
				// deleted on a single read would state that as fact.
				return nil, State{Detail: "pod not listed in the last read"}, false, nil
			}
			// gone from two reads in a row: terminated out of band, or the account
			// ran out of credit. waiting for it is pointless, and claiming it is
			// still billing would be a lie.
			return nil, State{}, false, &FatalError{
				Code: "not_found",
				Err:  fmt.Errorf("pod %s is no longer listed, so it was terminated or deleted while waiting", f.podID),
			}
		}
		// a just-created pod can lag the list query, so an absence here starts
		// out benign -- but not indefinitely. a pod terminated before it was
		// ever listed (an account out of credit is the realistic way) would
		// otherwise be waited out to the deadline and reported as a
		// wait_timeout, which reads as "try again" for something that never
		// will. same bound and same reasoning as the endpoint wait.
		f.missed++
		if f.missed >= missesBeforeKnown {
			return nil, State{}, false, &FatalError{
				Code: "not_found",
				// not "it was never created": the id came from a create that
				// succeeded, and saying otherwise invites a retry that buys a
				// second pod.
				Err: fmt.Errorf("pod %s has not appeared in %d consecutive reads, so it was terminated before it was ever listed, or it is not visible to this api key", f.podID, f.missed),
			}
		}
		return nil, State{Detail: "pod not listed yet"}, false, nil
	}
	f.seen = true
	f.missed = 0

	if pod.Runtime == nil {
		if reader, ok := f.lister.(PodReader); ok {
			// use the single-pod snapshot whole, status and runtime together, so
			// a pod that exited between the two reads is judged on its fresh
			// status. a failed re-read keeps the listed view and polls again.
			if fresh, err := reader.GetPodRuntime(f.podID); err == nil && fresh != nil && fresh.ID == f.podID {
				pod = fresh
			}
		}
	}

	if terminalPodStatuses[strings.ToUpper(pod.DesiredStatus)] {
		return nil, State{}, false, &FatalError{
			Code: "conflict",
			Err:  fmt.Errorf("pod %s is %s, so the wait cannot succeed", f.podID, strings.ToLower(pod.DesiredStatus)),
		}
	}

	return pod, State{}, true, nil
}

// PodSSHPoller polls until pod podID is running, has a public ssh port, and that
// port answers with an ssh banner. Pass a nil probe to use ProbeSSH.
//
// When addr is non-nil it receives the address the probe succeeded against, so a
// caller can report it even if a later read of the pod fails.
func PodSSHPoller(lister PodLister, podID string, probe Prober, addr *string) PollFunc {
	if probe == nil {
		probe = ProbeSSH
	}
	finder := &podFinder{lister: lister, podID: podID}
	return func(ctx context.Context) (State, error) {
		pod, st, found, err := finder.locate()
		if !found {
			return st, err
		}

		if !strings.EqualFold(pod.DesiredStatus, "RUNNING") {
			return State{Detail: "pod status " + strings.ToLower(pod.DesiredStatus)}, nil
		}

		ip, port, ok := sshconnect.PublicSSHPort(pod)
		if !ok {
			return State{Detail: "ssh port not allocated yet"}, nil
		}
		address := net.JoinHostPort(ip, strconv.Itoa(port))
		if err := probe(ctx, address); err != nil {
			return State{Detail: fmt.Sprintf("ssh port %s allocated but not reachable: %v", address, err)}, nil
		}
		if addr != nil {
			*addr = address
		}
		return State{Ready: true, Detail: "ssh reachable at " + address}, nil
	}
}

// PodRunningPoller polls until pod podID's container is actually up (desiredStatus
// RUNNING and runtime telemetry reported), never probing ssh. It exists for
// `--wait-for running`: an image whose sshd never starts (or a pod created with
// --ssh=false) can never satisfy PodSSHPoller and only ever times out. Readiness reuses the same running/initializing distinction as
// `pod get` (see internal/podstate), so it means the same thing everywhere.
func PodRunningPoller(lister PodLister, podID string) PollFunc {
	finder := &podFinder{lister: lister, podID: podID}
	return func(_ context.Context) (State, error) {
		pod, st, found, err := finder.locate()
		if !found {
			return st, err
		}

		switch sshconnect.PodState(pod).Status {
		case podstate.StatusRunning:
			return State{Ready: true, Detail: "pod " + podID + " is running"}, nil
		case podstate.StatusInitializing:
			return State{Detail: "pod running, container not up yet"}, nil
		default:
			return State{Detail: "pod status " + strings.ToLower(pod.DesiredStatus)}, nil
		}
	}
}
