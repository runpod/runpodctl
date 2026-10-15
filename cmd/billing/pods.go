package billing

import (
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var podsCmd = &cobra.Command{
	Use:   "pods",
	Short: "view pod billing history",
	Long:  "view billing history for pods, one record per pod per time bucket, split into gpu, cpu and disk cost",
	Args:  cobra.NoArgs,
	RunE:  runPodsBilling,
}

var (
	podsStartTime  string
	podsEndTime    string
	podsBucketSize string
	podsPodID      string
)

func init() {
	podsCmd.Flags().StringVar(&podsStartTime, "start-time", "", "start time (RFC3339 format)")
	podsCmd.Flags().StringVar(&podsEndTime, "end-time", "", "end time (RFC3339 format)")
	podsCmd.Flags().StringVar(&podsBucketSize, "bucket-size", "day", "bucket size (hour, day, week, month, year)")
	podsCmd.Flags().StringVar(&podsPodID, "pod-id", "", "filter by pod id")
}

func runPodsBilling(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	opts := &api.BillingOptions{
		StartTime:  podsStartTime,
		EndTime:    podsEndTime,
		BucketSize: podsBucketSize,
		PodID:      podsPodID,
	}

	records, err := client.GetPodBilling(opts)
	if err != nil {
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(records, &output.Config{Format: format})
}
