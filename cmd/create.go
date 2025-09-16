package cmd

import (
	"github.com/runpod/runpodctl/cmd/model"
	"github.com/runpod/runpodctl/cmd/pod"
	"github.com/runpod/runpodctl/cmd/pods"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create [command]",
	Short: "create a resource",
	Long:  "create a resource in runpod.io",
}

func init() {
	// Model repository command is hidden because the feature is still in development.
	createCmd.AddCommand(model.AddModelToRepoCmd)
	createCmd.AddCommand(pod.CreatePodCmd)
	createCmd.AddCommand(pods.CreatePodsCmd)
}
