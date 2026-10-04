package billing

import (
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var serverlessCmd = &cobra.Command{
	Use:     "serverless",
	Aliases: []string{"sls", "endpoints"},
	Short:   "view serverless billing history",
	Long:    "view billing history for serverless endpoints, one record per endpoint per time bucket, split into gpu, cpu and disk cost",
	Args:    cobra.NoArgs,
	RunE:    runServerlessBilling,
}

var (
	slsStartTime  string
	slsEndTime    string
	slsBucketSize string
	slsEndpointID string
)

func init() {
	serverlessCmd.Flags().StringVar(&slsStartTime, "start-time", "", "start time (RFC3339 format)")
	serverlessCmd.Flags().StringVar(&slsEndTime, "end-time", "", "end time (RFC3339 format)")
	serverlessCmd.Flags().StringVar(&slsBucketSize, "bucket-size", "day", "bucket size (hour, day, week, month, year)")
	serverlessCmd.Flags().StringVar(&slsEndpointID, "endpoint-id", "", "filter by endpoint id")
}

func runServerlessBilling(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	opts := &api.BillingOptions{
		StartTime:  slsStartTime,
		EndTime:    slsEndTime,
		BucketSize: slsBucketSize,
		EndpointID: slsEndpointID,
	}

	records, err := client.GetEndpointBilling(opts)
	if err != nil {
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(records, &output.Config{Format: format})
}
