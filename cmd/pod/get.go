package pod

import (
	"fmt"

	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"
	"github.com/runpod/runpod/internal/sshconnect"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get <pod-id>",
	Short: "get pod details",
	Long:  "get details for a specific pod by id",
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
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
		output.Error(err)
		return err
	}

	pod, err := client.GetPod(podID, getIncludeMachine, getIncludeNetworkVolume)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to get pod: %w", err)
	}

	sshInfo := map[string]interface{}{}
	gqlClient, err := api.NewGraphQLClient()
	if err == nil {
		pods, gqlErr := gqlClient.GetPods()
		if gqlErr == nil {
			keyInfo := sshconnect.ResolveKeyInfo(gqlClient)
			sshPod, conn := sshconnect.FindPodConnection(pods, podID, keyInfo)
			if sshPod != nil {
				if pod.LastStatusChange == nil && sshPod.LastStatusChange != nil {
					pod.LastStatusChange = sshPod.LastStatusChange
				}
				if pod.UptimeSeconds == nil && sshPod.UptimeSeconds != nil {
					pod.UptimeSeconds = sshPod.UptimeSeconds
				}
				if conn == nil {
					sshInfo = map[string]interface{}{
						"error":  "pod not ready",
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

	response := struct {
		*api.Pod
		SSH map[string]interface{} `json:"ssh"`
	}{
		Pod: pod,
		SSH: sshInfo,
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(response, &output.Config{Format: format})
}
