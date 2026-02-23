package volume

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create a new network volume",
	Long:  "create a new network volume",
	Args:  cobra.NoArgs,
	RunE:  runCreate,
}

var (
	createName         string
	createSize         int
	createDataCenterID string
)

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "volume name (required)")
	createCmd.Flags().IntVar(&createSize, "size", 0, "volume size in gb (1-4000, required)")
	createCmd.Flags().StringVar(&createDataCenterID, "data-center-id", "", "data center id (required)")

	createCmd.MarkFlagRequired("name")           //nolint:errcheck
	createCmd.MarkFlagRequired("size")           //nolint:errcheck
	createCmd.MarkFlagRequired("data-center-id") //nolint:errcheck
}

func runCreate(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	req := &api.NetworkVolumeCreateRequest{
		Name:         createName,
		Size:         createSize,
		DataCenterID: createDataCenterID,
	}

	volume, err := client.CreateNetworkVolume(req)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to create volume: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(volume, &output.Config{Format: format})
}
