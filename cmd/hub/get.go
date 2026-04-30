package hub

import (
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get <id-or-owner/name>",
	Short: "get hub repo details",
	Long: `get details for a hub repo by id or owner/name.

examples:
  runpodctl hub get clma1kziv00064iog9u6acj6z     # by listing id
  runpodctl hub get runpod/worker-vllm              # by owner/name`,
	Args: cobra.ExactArgs(1),
	RunE: runGet,
}

func runGet(cmd *cobra.Command, args []string) error {
	arg := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	var listing *api.Listing

	if strings.Contains(arg, "/") {
		parts := strings.SplitN(arg, "/", 2)
		if parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid format %q; use owner/name or a listing id", arg)
		}
		listing, err = client.GetListingFromRepo(parts[0], parts[1])
	} else {
		listing, err = client.GetListing(arg)
	}

	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to get hub repo: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(listing, &output.Config{Format: format})
}
