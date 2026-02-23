package volume

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get <volume-id>",
	Short: "get volume details",
	Long:  "get details for a specific network volume by id",
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
}

func runGet(cmd *cobra.Command, args []string) error {
	volumeID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	volume, err := client.GetNetworkVolume(volumeID)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to get volume: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(volume, &output.Config{Format: format})
}
