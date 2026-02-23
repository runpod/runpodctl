package template

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get <template-id>",
	Short: "get template details",
	Long:  "get details for a specific template by id",
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
}

func runGet(cmd *cobra.Command, args []string) error {
	templateID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	template, err := client.GetTemplate(templateID)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to get template: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(template, &output.Config{Format: format})
}
