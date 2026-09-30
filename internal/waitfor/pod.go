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

// PodLister is the slice of the graphql client a pod wait needs. Only graphql
// returns runtime.ports (the rest read shape leaves runtime null on a running
// pod, verified against prod), so ssh readiness has to come from here.
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
// the running-desired, non-terminal pod (found == true), or a State/error the
// poller must return verbatim (found == false). A returned error is either a
// transient read error or a *FatalError that ends the wait.
func (f *podFinder) locate() (pod *api.LegacyPod, st State, err error, found bool) {
	pods, listErr := f.lister.GetPods()
	if listErr != nil {
		// a failed read is an unknown state, not an absence, and it breaks the
		// run of consecutive misses -- otherwise a short list, a graphql blip and
		// a second short list would add up to a not_found for a live pod.
		f.missed = 0
		return nil, State{}, listErr, false
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
				return nil, State{Detail: "pod not listed in the last read"}, nil, false
			}
			// gone from two reads in a row: terminated out of band, or the account
			// ran out of credit. waiting for it is pointless, and claiming it is
			// still billing would be a lie.
			return nil, State{}, &FatalError{
				Code: "not_found",
				Err:  fmt.Errorf("pod %s is no longer listed, so it was terminated or deleted while waiting", f.podID),
			}, false
		}
		// a just-created pod can lag the list query, so an absence here starts
		// out benign -- but not indefinitely. a pod terminated before it was
		// ever listed (an account out of credit is the realistic way) would
		// otherwise be waited out to the deadline and reported as a
		// wait_timeout, which reads as "try again" for something that never
		// will. same bound and same reasoning as the endpoint wait.
		f.missed++
		if f.missed >= missesBeforeKnown {
			return nil, State{}, &FatalError{
				Code: "not_found",
				// not "it was never created": the id came from a create that
				// succeeded, and saying otherwise invites a retry that buys a
				// second pod.
				Err: fmt.Errorf("pod %s has not appeared in %d consecutive reads, so it was terminated before it was ever listed, or it is not visible to this api key", f.podID, f.missed),
			}, false
		}
		return nil, State{Detail: "pod not listed yet"}, nil, false
	}
	f.seen = true
	f.missed = 0

	if terminalPodStatuses[strings.ToUpper(pod.DesiredStatus)] {
		return nil, State{}, &FatalError{
			Code: "conflict",
			Err:  fmt.Errorf("pod %s is %s, so the wait cannot succeed", f.podID, strings.ToLower(pod.DesiredStatus)),
		}, false
	}

	return pod, State{}, nil, true
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
		pod, st, err, found := finder.locate()
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
// `--wait-for running`: a cpu pod is created over rest with no runpod-managed ssh,
// so an image without its own sshd can never satisfy PodSSHPoller and only ever
// times out. Readiness reuses the same running/initializing distinction as
// `pod get` (see internal/podstate), so it means the same thing everywhere.
func PodRunningPoller(lister PodLister, podID string) PollFunc {
	finder := &podFinder{lister: lister, podID: podID}
	return func(ctx context.Context) (State, error) {
		pod, st, err, found := finder.locate()
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
