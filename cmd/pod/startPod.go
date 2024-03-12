package pod

import (
	"fmt"

	"github.com/runpod/runpodctl/api"

	"github.com/spf13/cobra"
)

var bidPerGpu float32

var StartPodCmd = &cobra.Command{
	Use:   "pod [podId]",
	Args:  cobra.ExactArgs(1),
	Short: "start a pod",
	Long:  "start a pod from runpod.io",
	Run: func(cmd *cobra.Command, args []string) {
		var err error
		var pod map[string]interface{}
		if bidPerGpu > 0 {
			pod, err = api.StartSpotPod(args[0], bidPerGpu)
		} else {
			pod, err = api.StartOnDemandPod(args[0])
		}
		cobra.CheckErr(err)

		if pod["desiredStatus"] == "RUNNING" {
			fmt.Printf(`pod "%s" started with $%.3f / hr`, args[0], pod["costPerHr"])
			fmt.Println()
		} else {
			cobra.CheckErr(fmt.Errorf(`pod "%s" start failed; status is %s`, args[0], pod["desiredStatus"]))
		}
	},
}

func init() {
	StartPodCmd.Flags().Float32Var(&bidPerGpu, "bid", 0, "bid per gpu for spot price")
}
