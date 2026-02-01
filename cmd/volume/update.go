package volume

import (
	"fmt"

	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <volume-id>",
	Short: "update a network volume",
	Long:  "update an existing network volume",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpdate,
}

var (
	updateName string
	updateSize int
)

func init() {
	updateCmd.Flags().StringVar(&updateName, "name", "", "new volume name")
	updateCmd.Flags().IntVar(&updateSize, "size", 0, "new volume size in gb (must be larger than current)")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	volumeID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	req := &api.NetworkVolumeUpdateRequest{}

	if updateName != "" {
		req.Name = updateName
	}
	if updateSize > 0 {
		req.Size = updateSize
	}

	volume, err := client.UpdateNetworkVolume(volumeID, req)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to update volume: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(volume, &output.Config{Format: format})
}
