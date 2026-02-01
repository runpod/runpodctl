package serverless

import (
	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "list all endpoints",
	Long:  "list all serverless endpoints in your account",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

var (
	listIncludeTemplate bool
	listIncludeWorkers  bool
)

func init() {
	listCmd.Flags().BoolVar(&listIncludeTemplate, "include-template", false, "include template info")
	listCmd.Flags().BoolVar(&listIncludeWorkers, "include-workers", false, "include workers info")
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	opts := &api.EndpointListOptions{
		IncludeTemplate: listIncludeTemplate,
		IncludeWorkers:  listIncludeWorkers,
	}

	endpoints, err := client.ListEndpoints(opts)
	if err != nil {
		output.Error(err)
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(endpoints, &output.Config{Format: format})
}
