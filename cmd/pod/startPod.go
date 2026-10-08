package pod

import (
	"fmt"

	"github.com/runpod/runpodctl/api"
	internalapi "github.com/runpod/runpodctl/internal/api"

	"github.com/spf13/cobra"
)

var bidPerGpu float32

var StartPodCmd = &cobra.Command{
	Use:   "pod [podId]",
	Args:  cobra.ExactArgs(1),
	Short: "start a pod",
	Long:  "start a pod from runpod.io",
	Run: func(cmd *cobra.Command, args []string) {
		var status string
		var costPerHr float64
		if bidPerGpu > 0 {
			// rp-migrate: keep-v1 -- rest v2 has no spot bids
			pod, err := api.StartSpotPod(args[0], bidPerGpu, gpuCount)
			cobra.CheckErr(err)
			status, _ = pod["desiredStatus"].(string)
			costPerHr, _ = pod["costPerHr"].(float64)
		} else {
			client, err := internalapi.NewClient()
			cobra.CheckErr(err)
			pod, err := client.StartPod(args[0])
			cobra.CheckErr(err)
			status, costPerHr = pod.DesiredStatus, pod.CostPerHr
		}

		if status == "RUNNING" {
			fmt.Printf(`pod "%s" started with $%.3f / hr`, args[0], costPerHr)
			fmt.Println()
		} else {
			cobra.CheckErr(fmt.Errorf(`pod "%s" start failed; status is %s`, args[0], status))
		}
	},
}

func init() {
	StartPodCmd.Flags().Float32Var(&bidPerGpu, "bid", 0, "bid per gpu for spot price")
	StartPodCmd.Flags().IntVar(&gpuCount, "gpuCount", 1, "number of GPUs to request")
}
