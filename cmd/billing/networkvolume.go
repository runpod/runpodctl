package billing

import (
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var networkVolumeCmd = &cobra.Command{
	Use:     "network-volume",
	Aliases: []string{"nv"},
	Short:   "view network volume billing history",
	Long:    "view billing history for network volumes",
	Args:    cobra.NoArgs,
	RunE:    runNetworkVolumeBilling,
}

var (
	nvStartTime  string
	nvEndTime    string
	nvBucketSize string
)

func init() {
	networkVolumeCmd.Flags().StringVar(&nvStartTime, "start-time", "", "start time (RFC3339 format)")
	networkVolumeCmd.Flags().StringVar(&nvEndTime, "end-time", "", "end time (RFC3339 format)")
	networkVolumeCmd.Flags().StringVar(&nvBucketSize, "bucket-size", "day", "bucket size (hour, day, week, month, year)")
}

func runNetworkVolumeBilling(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	opts := &api.BillingOptions{
		StartTime:  nvStartTime,
		EndTime:    nvEndTime,
		BucketSize: nvBucketSize,
	}

	records, err := client.GetNetworkVolumeBilling(opts)
	if err != nil {
		output.Error(err)
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(records, &output.Config{Format: format})
}
