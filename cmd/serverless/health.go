package serverless

import (
	"context"
	"fmt"

	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health <endpoint-id>",
	Short: "get endpoint health",
	Long: `get the health of a serverless endpoint: worker counts by state and job counts by outcome.

the response comes from the invoke api verbatim, so new fields appear without a cli update.

examples:
  runpodctl serverless health <endpoint-id>`,
	Args: cobra.ExactArgs(1),
	RunE: runHealth,
}

func runHealth(cmd *cobra.Command, args []string) error {
	endpointID := args[0]

	client, err := newInvokeClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout())
	defer cancel()

	health, err := client.EndpointHealth(ctx, endpointID)
	if err != nil {
		return fmt.Errorf("failed to get endpoint health: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.PrintRaw(health, &output.Config{Format: format})
}
