package cmd

import (
	"github.com/runpod/runpod/cmd/cloud"
	"github.com/runpod/runpod/cmd/model"
	"github.com/runpod/runpod/cmd/pod"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get [command]",
	Short: "get resource",
	Long:  "get resources for pods",
}

func init() {
	// Model repository command is hidden because the feature is still in development.
	getCmd.AddCommand(cloud.GetCloudCmd)
	getCmd.AddCommand(model.GetModelsCmd)
	getCmd.AddCommand(pod.GetPodCmd)
}
