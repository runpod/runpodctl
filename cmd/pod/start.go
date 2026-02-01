package pod

import (
	"fmt"

	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start <pod-id>",
	Short: "start a stopped pod",
	Long:  "start a stopped pod by id",
	Args:  cobra.ExactArgs(1),
	RunE:  runStart,
}

func runStart(cmd *cobra.Command, args []string) error {
	podID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	pod, err := client.StartPod(podID)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to start pod: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(pod, &output.Config{Format: format})
}
