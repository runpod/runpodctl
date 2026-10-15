package pod

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"

	"github.com/spf13/cobra"
)

var RemovePodCmd = &cobra.Command{
	Use:   "pod [podId]",
	Args:  cobra.ExactArgs(1),
	Short: "remove a pod",
	Long:  "remove a pod from runpod.io",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := api.NewClient()
		cobra.CheckErr(err)
		cobra.CheckErr(client.DeletePod(args[0]))

		fmt.Printf(`pod "%s" removed`, args[0])
		fmt.Println()
	},
}
