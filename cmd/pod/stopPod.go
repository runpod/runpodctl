package pod

import (
	"cli/api"
	"fmt"

	"github.com/spf13/cobra"
)

var StopPodCmd = &cobra.Command{
	Use:     "pod [podId]",
	Aliases: []string{"pods"},
	Args:    cobra.ExactArgs(1),
	Short:   "get all pods",
	Long:    "get all pods or specify pod id",
	Run: func(cmd *cobra.Command, args []string) {
		pod, err := api.PodStop(args[0])
		cobra.CheckErr(err)

		if pod["desiredStatus"] == "EXITED" {
			fmt.Printf(`pod "%s" stopped`, args[0])
		} else {
			fmt.Printf(`pod "%s" stop failed; status is %s`, args[0], pod["desiredStatus"])
		}
		fmt.Println()
	},
}
