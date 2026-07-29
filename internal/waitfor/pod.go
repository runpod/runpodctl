package waitfor

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
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

// PodSSHPoller polls until pod podID is running, has a public ssh port, and that
// port answers with an ssh banner. Pass a nil probe to use ProbeSSH.
//
// When addr is non-nil it receives the address the probe succeeded against, so a
// caller can report it even if a later read of the pod fails.
func PodSSHPoller(lister PodLister, podID string, probe Prober, addr *string) PollFunc {
	if probe == nil {
		probe = ProbeSSH
	}
	seen := false
	return func(ctx context.Context) (State, error) {
		pods, err := lister.GetPods()
		if err != nil {
			return State{}, err
		}

		var pod *api.LegacyPod
		for _, candidate := range pods {
			if candidate != nil && candidate.ID == podID {
				pod = candidate
				break
			}
		}
		if pod == nil {
			if seen {
				// it was there and now it is not: terminated out of band, or the
				// account ran out of credit. waiting for it is pointless, and claiming
				// it is still billing would be a lie.
				return State{}, &FatalError{
					Code: "not_found",
					Err:  fmt.Errorf("pod %s is no longer listed, so it was terminated or deleted while waiting", podID),
				}
			}
			// a just-created pod can lag the list query; not an error.
			return State{Detail: "pod not listed yet"}, nil
		}
		seen = true

		if terminalPodStatuses[strings.ToUpper(pod.DesiredStatus)] {
			return State{}, &FatalError{
				Code: "conflict",
				Err:  fmt.Errorf("pod %s is %s, so it will never become reachable", podID, strings.ToLower(pod.DesiredStatus)),
			}
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
