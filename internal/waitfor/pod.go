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

// PodSSHPoller polls until pod podID is running, has a public ssh port, and that
// port answers with an ssh banner. Pass a nil probe to use ProbeSSH.
func PodSSHPoller(lister PodLister, podID string, probe Prober) PollFunc {
	if probe == nil {
		probe = ProbeSSH
	}
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
			// a just-created pod can lag the list query; not an error.
			return State{Detail: "pod not listed yet"}, nil
		}
		if !strings.EqualFold(pod.DesiredStatus, "RUNNING") {
			return State{Detail: "pod status " + strings.ToLower(pod.DesiredStatus)}, nil
		}

		ip, port, ok := sshconnect.PublicSSHPort(pod)
		if !ok {
			return State{Detail: "ssh port not allocated yet"}, nil
		}
		addr := net.JoinHostPort(ip, strconv.Itoa(port))
		if err := probe(ctx, addr); err != nil {
			return State{Detail: fmt.Sprintf("ssh port %s allocated but not reachable: %v", addr, err)}, nil
		}
		return State{Ready: true, Detail: "ssh reachable at " + addr}, nil
	}
}
