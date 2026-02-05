package billing

import (
	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var podsCmd = &cobra.Command{
	Use:   "pods",
	Short: "view pod billing history",
	Long:  "view billing history for gpu pods",
	Args:  cobra.NoArgs,
	RunE:  runPodsBilling,
}

var (
	podsStartTime  string
	podsEndTime    string
	podsBucketSize string
	podsGrouping   string
	podsPodID      string
	podsGpuTypeID  string
)

func init() {
	podsCmd.Flags().StringVar(&podsStartTime, "start-time", "", "start time (RFC3339 format)")
	podsCmd.Flags().StringVar(&podsEndTime, "end-time", "", "end time (RFC3339 format)")
	podsCmd.Flags().StringVar(&podsBucketSize, "bucket-size", "day", "bucket size (hour, day, week, month, year)")
	podsCmd.Flags().StringVar(&podsGrouping, "grouping", "gpuId", "grouping (podId, gpuId)")
	podsCmd.Flags().StringVar(&podsPodID, "pod-id", "", "filter by pod id")
	podsCmd.Flags().StringVar(&podsGpuTypeID, "gpu-id", "", "filter by gpu id")
}

func runPodsBilling(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	grouping := normalizeGpuGrouping(podsGrouping)
	opts := &api.BillingOptions{
		StartTime:  podsStartTime,
		EndTime:    podsEndTime,
		BucketSize: podsBucketSize,
		Grouping:   grouping,
		PodID:      podsPodID,
		GpuTypeID:  podsGpuTypeID,
	}

	records, err := client.GetPodBilling(opts)
	if err != nil {
		output.Error(err)
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(records, &output.Config{Format: format})
}
