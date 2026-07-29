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
cannot: running (container up), initializing (placed, container not up yet -
image pull, create or boot), stopped, terminated, or unknown.
runtimeStatusReason carries a stable token when there is more to say.`,
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
	sshInfo := map[string]interface{}{}
	signals := podstate.Signals{
		DesiredStatus:    pod.DesiredStatus,
		LastStatusChange: pod.LastStatusChange,
	}

	gqlClient, err := api.NewGraphQLClient()
	if err == nil {
		pods, gqlErr := gqlClient.GetPods()
		if gqlErr == nil {
			keyInfo := sshconnect.ResolveKeyInfo(gqlClient)
			sshPod, conn := sshconnect.FindPodConnection(pods, podID, keyInfo)
			if sshPod != nil {
				signals.RuntimeProbed = true
				signals.RuntimeReported = sshPod.Runtime != nil
				if pod.LastStatusChange == nil && sshPod.LastStatusChange != nil {
					pod.LastStatusChange = sshPod.LastStatusChange
					signals.LastStatusChange = sshPod.LastStatusChange
				}
				pod.UptimeSeconds = runtimeUptime(sshPod.Runtime)
				if conn == nil {
					sshInfo = map[string]interface{}{
						"error":  notReadyMessage(podstate.Derive(signals)),
						"id":     sshPod.ID,
						"name":   sshPod.Name,
						"status": sshPod.DesiredStatus,
					}
				} else {
					sshInfo = conn
				}
			} else {
				sshInfo = map[string]interface{}{"error": "ssh info unavailable"}
			}
		} else {
			sshInfo = map[string]interface{}{"error": "ssh info unavailable"}
		}
	} else {
		sshInfo = map[string]interface{}{"error": "ssh info unavailable"}
	}

	state := podstate.Derive(signals)

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

// runtimeUptime returns the only uptime the api actually reports. The deprecated
// top-level Pod.uptimeSeconds is 0 for every pod in prod and rest omits the
// field entirely, so the old merge published a permanent `"uptimeSeconds": 0`
// even on a pod that had been up for minutes. Returning nil while the container
// is not reporting leaves the field out, which is honest; 0 was not.
func runtimeUptime(runtime *api.LegacyRuntime) interface{} {
	if runtime == nil || runtime.UptimeInSeconds == nil {
		return nil
	}
	return *runtime.UptimeInSeconds
}

// notReadyMessage turns "pod not ready" — the bare string that used to be the
// only thing an agent got while an image was pulling — into a message that says
// which of the possible causes applies.
func notReadyMessage(state podstate.State) string {
	if reason := podstate.SSHUnavailableReason(state); reason != "" {
		return "pod not ready: " + reason
	}
	return "pod not ready"
}
