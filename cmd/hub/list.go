package hub

import (
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "list hub repos",
	Long: `list repos from the runpod hub.

by default shows the top 10 repos ordered by stars.

examples:
  runpodctl hub list                              # top 10 by stars
  runpodctl hub list --type SERVERLESS            # only serverless repos
  runpodctl hub list --type POD                   # only pod repos
  runpodctl hub list --category ai --limit 20     # filter by category
  runpodctl hub list --order-by deploys           # order by deploys
  runpodctl hub list --owner runpod               # filter by repo owner`,
	Args: cobra.NoArgs,
	RunE: runList,
}

var (
	listCategory string
	listOrderBy  string
	listOrderDir string
	listLimit    int
	listOffset   int
	listOwner    string
	listType     string
)

func init() {
	listCmd.Flags().StringVar(&listCategory, "category", "", "filter by category")
	listCmd.Flags().StringVar(&listOrderBy, "order-by", "stars", "order by: createdAt, deploys, releasedAt, stars, updatedAt, views")
	listCmd.Flags().StringVar(&listOrderDir, "order-dir", "desc", "order direction: asc or desc")
	listCmd.Flags().IntVar(&listLimit, "limit", 10, "max number of results to return")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "offset for pagination")
	listCmd.Flags().StringVar(&listOwner, "owner", "", "filter by repo owner")
	listCmd.Flags().StringVar(&listType, "type", "", "filter by type: POD or SERVERLESS (applied client-side; --limit may return fewer results)")
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	opts := &api.ListingsOptions{
		Category:       listCategory,
		OrderBy:        listOrderBy,
		OrderDirection: listOrderDir,
		Limit:          listLimit,
		Offset:         listOffset,
		Owner:          listOwner,
		Type:           listType,
	}

	listings, err := client.ListListings(opts)
	if err != nil {
		output.Error(err)
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(listings, &output.Config{Format: format})
}
