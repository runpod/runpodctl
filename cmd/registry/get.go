package registry

import (
	"fmt"

	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get <registry-auth-id>",
	Short: "get registry auth details",
	Long:  "get details for a specific container registry auth by id",
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
}

func runGet(cmd *cobra.Command, args []string) error {
	authID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	auth, err := client.GetContainerRegistryAuth(authID)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to get registry auth: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(auth, &output.Config{Format: format})
}
