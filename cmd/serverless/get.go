package serverless

import (
	"fmt"

	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get <endpoint-id>",
	Short: "get endpoint details",
	Long:  "get details for a specific endpoint by id",
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
}

var (
	getIncludeTemplate bool
	getIncludeWorkers  bool
)

func init() {
	getCmd.Flags().BoolVar(&getIncludeTemplate, "include-template", false, "include template info")
	getCmd.Flags().BoolVar(&getIncludeWorkers, "include-workers", false, "include workers info")
}

func runGet(cmd *cobra.Command, args []string) error {
	endpointID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	endpoint, err := client.GetEndpoint(endpointID, getIncludeTemplate, getIncludeWorkers)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to get endpoint: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(endpoint, &output.Config{Format: format})
}
