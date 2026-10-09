package pod

import (
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <pod-id>",
	Short: "restart a pod",
	Long:  "restart a running pod",
	Args:  cobra.ExactArgs(1),
	RunE:  runRestart,
}

func runRestart(cmd *cobra.Command, args []string) error {
	podID := args[0]

	client, err := api.NewV2Client()
	if err != nil {
		return err
	}

	pod, err := client.RestartPodV2(podID)
	if err != nil {
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(pod, &output.Config{Format: format})
}
