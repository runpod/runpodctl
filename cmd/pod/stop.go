package pod

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop <pod-id>",
	Short: "stop a running pod",
	Long:  "stop a running pod by id",
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
	podID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	pod, err := client.StopPod(podID)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to stop pod: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(pod, &output.Config{Format: format})
}
