package pod

import (
	"fmt"

	"github.com/runpod/runpodctl/api"

	"github.com/spf13/cobra"
)

var StopPodCmd = &cobra.Command{
	Use:   "pod [podId]",
	Args:  cobra.ExactArgs(1),
	Short: "stop a pod",
	Long:  "stop a pod from runpod.io",
	Run: func(cmd *cobra.Command, args []string) {
		pod, err := api.StopPod(args[0])
		cobra.CheckErr(err)

		if pod["desiredStatus"] == "EXITED" {
			fmt.Printf(`pod "%s" stopped`, args[0])
		} else {
			fmt.Printf(`pod "%s" stop failed; status is %s`, args[0], pod["desiredStatus"])
		}
		fmt.Println()
	},
}
