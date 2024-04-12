package cmd

import (
	"github.com/runpod/runpodctl/cmd/pod"
	"github.com/runpod/runpodctl/cmd/pods"

	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove [command]",
	Short: "remove a resource",
	Long:  "remove a resource in runpod.io",
}

func init() {
	removeCmd.AddCommand(pod.RemovePodCmd)
	removeCmd.AddCommand(pods.RemovePodsCmd)
}
