package cmd

import (
	"github.com/runpod/runpod/cmd/model"
	"github.com/runpod/runpod/cmd/pod"
	"github.com/runpod/runpod/cmd/pods"

	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove [command]",
	Short: "remove a resource",
	Long:  "remove a resource in runpod.io",
}

func init() {
	// Model repository command is hidden because the feature is still in development.
	removeCmd.AddCommand(model.RemoveModelCmd)
	removeCmd.AddCommand(pod.RemovePodCmd)
	removeCmd.AddCommand(pods.RemovePodsCmd)
}
