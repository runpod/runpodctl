package template

import (
	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "list templates",
	Long: `list templates including official, community, and user templates.

by default shows official + community templates (limited to 10).
use 'runpodctl template search <term>' to search for specific templates.

examples:
  runpodctl template list                      # official + community (first 10)
  runpodctl template list --type official      # all official templates (no limit)
  runpodctl template list --type community     # community templates (first 10)
  runpodctl template list --type user          # all your own templates (no limit)
  runpodctl template list --all                # everything including user templates
  runpodctl template list --limit 50           # show 50 templates`,
	Args: cobra.NoArgs,
	RunE: runList,
}

var (
	listType   string
	listLimit  int
	listOffset int
	listAll    bool
)

func init() {
	listCmd.Flags().StringVar(&listType, "type", "", "filter by type: official, community, user (default: official+community)")
	listCmd.Flags().IntVar(&listLimit, "limit", 10, "max number of templates to return")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "offset for pagination")
	listCmd.Flags().BoolVar(&listAll, "all", false, "include user templates (same as --type all)")
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	// Handle type: --all flag sets type to "all" (includes user templates)
	templateType := api.TemplateType(listType)
	limit := listLimit

	// Determine limit based on type:
	// - user and official: bounded sets, show all by default
	// - community and default: large sets, apply limit
	limitExplicitlySet := cmd.Flags().Changed("limit")

	if listAll {
		templateType = api.TemplateTypeAll
		limit = 0 // no limit when --all is used
	} else if !limitExplicitlySet {
		// Only apply smart defaults if user didn't explicitly set --limit
		switch templateType {
		case api.TemplateTypeUser, api.TemplateTypeOfficial:
			limit = 0 // bounded sets, show all
		default:
			// community or default (official+community): keep default limit
			limit = listLimit
		}
	}

	opts := &api.TemplateListOptions{
		Type:   templateType,
		Offset: listOffset,
		Limit:  limit,
	}

	templates, err := client.ListAllTemplates(opts)
	if err != nil {
		output.Error(err)
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(templates, &output.Config{Format: format})
}
