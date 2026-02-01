package billing

import (
	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var serverlessCmd = &cobra.Command{
	Use:     "serverless",
	Aliases: []string{"sls", "endpoints"},
	Short:   "view serverless billing history",
	Long:    "view billing history for serverless endpoints",
	Args:    cobra.NoArgs,
	RunE:    runServerlessBilling,
}

var (
	slsStartTime  string
	slsEndTime    string
	slsBucketSize string
	slsGrouping   string
	slsEndpointID string
	slsGpuTypeID  string
)

func init() {
	serverlessCmd.Flags().StringVar(&slsStartTime, "start-time", "", "start time (RFC3339 format)")
	serverlessCmd.Flags().StringVar(&slsEndTime, "end-time", "", "end time (RFC3339 format)")
	serverlessCmd.Flags().StringVar(&slsBucketSize, "bucket-size", "day", "bucket size (hour, day, week, month, year)")
	serverlessCmd.Flags().StringVar(&slsGrouping, "grouping", "endpointId", "grouping (endpointId, podId, gpuTypeId)")
	serverlessCmd.Flags().StringVar(&slsEndpointID, "endpoint-id", "", "filter by endpoint id")
	serverlessCmd.Flags().StringVar(&slsGpuTypeID, "gpu-type-id", "", "filter by gpu type id")
}

func runServerlessBilling(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	opts := &api.BillingOptions{
		StartTime:  slsStartTime,
		EndTime:    slsEndTime,
		BucketSize: slsBucketSize,
		Grouping:   slsGrouping,
		EndpointID: slsEndpointID,
		GpuTypeID:  slsGpuTypeID,
	}

	records, err := client.GetEndpointBilling(opts)
	if err != nil {
		output.Error(err)
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(records, &output.Config{Format: format})
}
