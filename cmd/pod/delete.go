package pod

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete <pod-id>",
	Aliases: []string{"rm", "remove"},
	Short:   "delete a pod",
	Long:    "delete/terminate a pod by id",
	Args:    cobra.ExactArgs(1),
	RunE:    runDelete,
}

func runDelete(cmd *cobra.Command, args []string) error {
	podID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	if err := client.DeletePod(podID); err != nil {
		output.Error(err)
		return fmt.Errorf("failed to delete pod: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(map[string]interface{}{
		"deleted": true,
		"id":      podID,
	}, &output.Config{Format: format})
}
