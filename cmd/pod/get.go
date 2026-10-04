package pod

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"
	"github.com/runpod/runpodctl/internal/podstate"
	"github.com/runpod/runpodctl/internal/sshconnect"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get <pod-id>",
	Short: "get pod details",
	Long: `get details for a specific pod by id.

runtimeStatus reports what the pod is actually doing, which desiredStatus
cannot: running (container up and reporting), initializing (no container
reported yet - image pull, create or boot), stopped, terminated, or unknown
(not derivable, read desiredStatus). runtimeStatusReason carries a stable
token when there is more to say, and lastStatusChange carries the backend's
raw text.`,
	Args: cobra.ExactArgs(1),
	RunE: runGet,
}

var (
	getIncludeMachine       bool
	getIncludeNetworkVolume bool
)

func init() {
	getCmd.Flags().BoolVar(&getIncludeMachine, "include-machine", false, "include machine info")
	getCmd.Flags().BoolVar(&getIncludeNetworkVolume, "include-network-volume", false, "include network volume info")
}

func runGet(cmd *cobra.Command, args []string) error {
	details, err := fetchPodDetails(args[0], getIncludeMachine, getIncludeNetworkVolume)
	if err != nil {
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(details, &output.Config{Format: format})
}

// fetchPodDetails reads a pod and enriches it with the derived runtime state and
// the live ssh connection info, all from the one rest v2 read.
//
// `pod create --wait` prints this same shape once ssh is up, instead of the
// create response: the point of waiting is to hand back a pod you can connect
// to, and neither create response carries the ssh command.
func fetchPodDetails(podID string, includeMachine, includeNetworkVolume bool) (*podDetails, error) {
	client, err := api.NewClient()
	if err != nil {
		return nil, err
	}

	pod, err := client.GetPod(podID, includeMachine, includeNetworkVolume)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	// rest v2 reports runtime telemetry and port mappings in the same read, so
	// the derived state, uptime and ssh block all come from one snapshot.
	sshInfo := map[string]interface{}{"error": "ssh info unavailable"}
	state := podstate.Derive(podstate.Signals{DesiredStatus: pod.DesiredStatus})

	if view := pod.Legacy(); view != nil {
		keyInfo := sshconnect.ResolveKeyInfo(client)
		sshPod, conn := sshconnect.FindPodConnection([]*api.LegacyPod{view}, podID, keyInfo)
		if sshPod != nil {
			state = sshconnect.PodState(sshPod)
			pod.UptimeSeconds = runtimeUptime(state, sshPod.Runtime)
			// A stopped pod keeps reporting stale runtime ports for a while,
			// which is enough for FindPodConnection to hand back an ssh
			// command that cannot possibly work.
			if conn == nil || state.IsKnownDown() {
				declared := pod.Ports
				if len(declared) == 0 {
					declared = sshconnect.SplitPorts(sshPod.Ports)
				}
				var runtimePorts []*api.LegacyPort
				if sshPod.Runtime != nil {
					runtimePorts = sshPod.Runtime.Ports
				}
				sshInfo = map[string]interface{}{
					"error":  sshconnect.NotReadyMessage(sshPod.ID, state, declared, runtimePorts),
					"id":     sshPod.ID,
					"name":   sshPod.Name,
					"status": sshPod.DesiredStatus,
				}
			} else {
				sshInfo = conn
			}
		}
	}

	return &podDetails{
		Pod:                 pod,
		RuntimeStatus:       string(state.Status),
		RuntimeStatusReason: string(state.Reason),
		SSH:                 sshInfo,
	}, nil
}

// podDetails is a pod read enriched with the derived runtime state and the live
// ssh block. It is the `pod get` payload, and the `pod create --wait` payload.
type podDetails struct {
	*api.Pod
	RuntimeStatus       string                 `json:"runtimeStatus"`
	RuntimeStatusReason string                 `json:"runtimeStatusReason,omitempty"`
	SSH                 map[string]interface{} `json:"ssh"`
}

// sshInfoMissing reports whether the ssh block is the degraded {"error": ...}
// placeholder rather than a usable connection.
func sshInfoMissing(details *podDetails) bool {
	if details == nil {
		return true
	}
	_, degraded := details.SSH["error"]
	return degraded
}

// sshInfoError returns the reason the ssh block is degraded. Only meaningful
// after sshInfoMissing returned true.
func sshInfoError(details *podDetails) string {
	if details == nil {
		return "no pod payload"
	}
	reason, _ := details.SSH["error"].(string)
	return reason
}

// runtimeUptime returns the only uptime the api actually reports, and only
// while it means something.
//
// The deprecated top-level Pod.uptimeSeconds is 0 for every pod in prod and rest
// omits the field entirely, so the old merge published a permanent
// `"uptimeSeconds": 0` even on a pod that had been up for minutes. Returning nil
// leaves the field out, which is honest; 0 was not.
//
// It is also gated on the pod being up: a stopped pod keeps reporting stale
// telemetry (observed: uptimeInSeconds frozen at its last value on an EXITED
// pod), and publishing that as uptime says the pod is running when it is not.
func runtimeUptime(state podstate.State, runtime *api.LegacyRuntime) interface{} {
	if state.Status != podstate.StatusRunning || runtime == nil || runtime.UptimeInSeconds == nil {
		return nil
	}
	return *runtime.UptimeInSeconds
}
