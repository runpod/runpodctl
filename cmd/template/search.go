package template

import (
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <term>",
	Short: "search templates",
	Long: `search for templates by name or image.

searches official and community templates by default.

examples:
  runpodctl template search pytorch            # search for "pytorch" templates
  runpodctl template search comfyui            # search for "comfyui" templates
  runpodctl template search llama --limit 5    # search, limit to 5 results
  runpodctl template search vllm --type official  # search only official templates`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

var (
	searchType   string
	searchLimit  int
	searchOffset int
)

func init() {
	searchCmd.Flags().StringVar(&searchType, "type", "", "filter by type: official, community, user (default: official+community)")
	searchCmd.Flags().IntVar(&searchLimit, "limit", 10, "max number of results to return")
	searchCmd.Flags().IntVar(&searchOffset, "offset", 0, "offset for pagination")
}

func runSearch(cmd *cobra.Command, args []string) error {
	searchTerm := args[0]

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	opts := &api.TemplateListOptions{
		Type:   api.TemplateType(searchType),
		Search: searchTerm,
		Offset: searchOffset,
		Limit:  searchLimit,
	}

	templates, err := client.ListAllTemplates(opts)
	if err != nil {
		return err
	}

	// an empty result set is still data: emit [] rather than prose on stdout, so a
	// caller piping this into a json parser does not break on "no X found".
	if templates == nil {
		templates = []api.Template{}
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(templates, &output.Config{Format: format})
}
