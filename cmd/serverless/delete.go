package serverless

import (
	"fmt"

	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete <endpoint-id>",
	Aliases: []string{"rm", "remove"},
	Short:   "delete an endpoint",
	Long:    "delete a serverless endpoint by id",
	Args:    cobra.ExactArgs(1),
	RunE:    runDelete,
}

func runDelete(cmd *cobra.Command, args []string) error {
	endpointID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	if err := client.DeleteEndpoint(endpointID); err != nil {
		output.Error(err)
		return fmt.Errorf("failed to delete endpoint: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(map[string]interface{}{
		"deleted": true,
		"id":      endpointID,
	}, &output.Config{Format: format})
}
