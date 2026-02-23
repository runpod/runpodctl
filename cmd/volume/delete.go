package volume

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete <volume-id>",
	Aliases: []string{"rm", "remove"},
	Short:   "delete a network volume",
	Long:    "delete a network volume by id",
	Args:    cobra.ExactArgs(1),
	RunE:    runDelete,
}

func runDelete(cmd *cobra.Command, args []string) error {
	volumeID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	if err := client.DeleteNetworkVolume(volumeID); err != nil {
		output.Error(err)
		return fmt.Errorf("failed to delete volume: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(map[string]interface{}{
		"deleted": true,
		"id":      volumeID,
	}, &output.Config{Format: format})
}
