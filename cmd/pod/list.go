package pod

import (
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "list all pods",
	Long:  "list all pods in your account",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

var (
	listComputeType          string
	listName                 string
	listIncludeMachine       bool
	listIncludeNetworkVolume bool
)

func init() {
	listCmd.Flags().StringVar(&listComputeType, "compute-type", "", "filter by compute type (GPU or CPU)")
	listCmd.Flags().StringVar(&listName, "name", "", "filter by pod name")
	listCmd.Flags().BoolVar(&listIncludeMachine, "include-machine", false, "include machine info")
	listCmd.Flags().BoolVar(&listIncludeNetworkVolume, "include-network-volume", false, "include network volume info")
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	opts := &api.PodListOptions{
		ComputeType:          listComputeType,
		Name:                 listName,
		IncludeMachine:       listIncludeMachine,
		IncludeNetworkVolume: listIncludeNetworkVolume,
	}

	pods, err := client.ListPods(opts)
	if err != nil {
		output.Error(err)
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(pods, &output.Config{Format: format})
}
