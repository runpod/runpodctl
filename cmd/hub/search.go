package hub

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <term>",
	Short: "search hub repos",
	Long: `search for repos in the runpod hub.

examples:
  runpodctl hub search vllm                        # search for "vllm"
  runpodctl hub search whisper --type SERVERLESS    # search serverless repos
  runpodctl hub search stable-diffusion --limit 5   # limit results`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

var (
	searchCategory string
	searchOrderBy  string
	searchOrderDir string
	searchLimit    int
	searchOffset   int
	searchOwner    string
	searchType     string
)

func init() {
	searchCmd.Flags().StringVar(&searchCategory, "category", "", "filter by category")
	searchCmd.Flags().StringVar(&searchOrderBy, "order-by", "stars", "order by: createdAt, deploys, releasedAt, stars, updatedAt, views")
	searchCmd.Flags().StringVar(&searchOrderDir, "order-dir", "desc", "order direction: asc or desc")
	searchCmd.Flags().IntVar(&searchLimit, "limit", 10, "max number of results to return")
	searchCmd.Flags().IntVar(&searchOffset, "offset", 0, "offset for pagination")
	searchCmd.Flags().StringVar(&searchOwner, "owner", "", "filter by repo owner")
	searchCmd.Flags().StringVar(&searchType, "type", "", "filter by type: POD or SERVERLESS (applied client-side; --limit may return fewer results)")
}

func runSearch(cmd *cobra.Command, args []string) error {
	searchTerm := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	opts := &api.ListingsOptions{
		SearchQuery:    searchTerm,
		Category:       searchCategory,
		OrderBy:        searchOrderBy,
		OrderDirection: searchOrderDir,
		Limit:          searchLimit,
		Offset:         searchOffset,
		Owner:          searchOwner,
		Type:           searchType,
	}

	listings, err := client.ListListings(opts)
	if err != nil {
		output.Error(err)
		return err
	}

	if len(listings) == 0 {
		fmt.Printf("no hub repos found matching %q\n", searchTerm)
		return nil
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(listings, &output.Config{Format: format})
}
