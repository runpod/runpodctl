package cmd

import (
	"github.com/runpod/runpodctl/cmd/cloud"
	"github.com/runpod/runpodctl/cmd/pod"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get [command]",
	Short: "get resource",
	Long:  "get resources for pods",
}

func init() {
	getCmd.AddCommand(cloud.GetCloudCmd)
	getCmd.AddCommand(pod.GetPodCmd)
}
