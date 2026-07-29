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
	podID := args[0]

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	pod, err := client.GetPod(podID, getIncludeMachine, getIncludeNetworkVolume)
	if err != nil {
		return fmt.Errorf("failed to get pod: %w", err)
	}

	// The graphql side-call below is the only source of runtime telemetry (rest
	// never returns `runtime` at all), and it was already being made for the ssh
	// block, so runtimeStatus costs no extra round trip here.
	sshInfo := map[string]interface{}{"error": "ssh info unavailable"}
	// Without the graphql snapshot there is no telemetry at all, so the honest
	// answer is unknown rather than a guess from desiredStatus alone.
	state := podstate.Derive(podstate.Signals{
		DesiredStatus:    pod.DesiredStatus,
		LastStatusChange: pod.LastStatusChange,
	})

	if gqlClient, gqlClientErr := api.NewGraphQLClient(); gqlClientErr == nil {
		if pods, gqlErr := gqlClient.GetPods(); gqlErr == nil {
			keyInfo := sshconnect.ResolveKeyInfo(gqlClient)
			sshPod, conn := sshconnect.FindPodConnection(pods, podID, keyInfo)
			if sshPod != nil {
				if pod.LastStatusChange == nil && sshPod.LastStatusChange != nil {
					pod.LastStatusChange = sshPod.LastStatusChange
				}
				// Derive from the graphql snapshot, not from rest's
				// desiredStatus: the runtime block and its ports come from this
				// snapshot, and gating them on a status read from the *other*
				// surface means momentary skew between the two bypasses the
				// gate and hands back an ssh command for a dead container.
				// rest's desiredStatus is still published as desiredStatus.
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
						"error":  sshconnect.NotReadyMessage(state, declared, runtimePorts),
						"id":     sshPod.ID,
						"name":   sshPod.Name,
						"status": sshPod.DesiredStatus,
					}
				} else {
					sshInfo = conn
				}
			}
		}
	}

	response := struct {
		*api.Pod
		RuntimeStatus       string                 `json:"runtimeStatus"`
		RuntimeStatusReason string                 `json:"runtimeStatusReason,omitempty"`
		SSH                 map[string]interface{} `json:"ssh"`
	}{
		Pod:                 pod,
		RuntimeStatus:       string(state.Status),
		RuntimeStatusReason: string(state.Reason),
		SSH:                 sshInfo,
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(response, &output.Config{Format: format})
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
