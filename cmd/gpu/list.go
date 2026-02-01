package gpu

import (
	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "list available gpu types",
	Long:  "list available gpu types with stock status",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

var includeUnavailable bool

func init() {
	listCmd.Flags().BoolVar(&includeUnavailable, "include-unavailable", false, "include gpus with no current availability")
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	gpus, err := client.ListGpuTypes(includeUnavailable)
	if err != nil {
		output.Error(err)
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(gpus, &output.Config{Format: format})
}
