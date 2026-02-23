package registry

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete <registry-auth-id>",
	Aliases: []string{"rm", "remove"},
	Short:   "delete a registry auth",
	Long:    "delete a container registry auth by id",
	Args:    cobra.ExactArgs(1),
	RunE:    runDelete,
}

func runDelete(cmd *cobra.Command, args []string) error {
	authID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	if err := client.DeleteContainerRegistryAuth(authID); err != nil {
		output.Error(err)
		return fmt.Errorf("failed to delete registry auth: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(map[string]interface{}{
		"deleted": true,
		"id":      authID,
	}, &output.Config{Format: format})
}
