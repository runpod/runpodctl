package template

import (
	"fmt"

	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete <template-id>",
	Aliases: []string{"rm", "remove"},
	Short:   "delete a template",
	Long:    "delete a template by id",
	Args:    cobra.ExactArgs(1),
	RunE:    runDelete,
}

func runDelete(cmd *cobra.Command, args []string) error {
	templateID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	if err := client.DeleteTemplate(templateID); err != nil {
		output.Error(err)
		return fmt.Errorf("failed to delete template: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(map[string]interface{}{
		"deleted": true,
		"id":      templateID,
	}, &output.Config{Format: format})
}
