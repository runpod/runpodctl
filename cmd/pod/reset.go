package pod

import (
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset <pod-id>",
	Short: "reset a pod",
	Long:  "reset a pod (stops and starts it)",
	Args:  cobra.ExactArgs(1),
	RunE:  runReset,
}

func runReset(cmd *cobra.Command, args []string) error {
	podID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	pod, err := client.ResetPod(podID)
	if err != nil {
		output.Error(err)
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(pod, &output.Config{Format: format})
}
