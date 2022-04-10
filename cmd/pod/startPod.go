package pod

import (
	"cli/api"
	"fmt"

	"github.com/spf13/cobra"
)

var bidPerGpu float32

var StartPodCmd = &cobra.Command{
	Use:     "pod [podId]",
	Aliases: []string{"pods"},
	Args:    cobra.ExactArgs(1),
	Short:   "get all pods",
	Long:    "get all pods or specify pod id",
	Run: func(cmd *cobra.Command, args []string) {
		var err error
		var pod map[string]interface{}
		if bidPerGpu > 0 {
			pod, err = api.PodBidResume(args[0], bidPerGpu)
		}
		cobra.CheckErr(err)

		if pod["desiredStatus"] == "RUNNING" {
			fmt.Printf(`pod "%s" started`, args[0])
			fmt.Println()
		} else {
			cobra.CheckErr(fmt.Errorf(`pod "%s" start failed; status is %s`, args[0], pod["desiredStatus"]))
		}
	},
}

func init() {
	StartPodCmd.Flags().Float32Var(&bidPerGpu, "bid", 0, "bid per gpu for spot price")
}
