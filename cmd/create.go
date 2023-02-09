package cmd

import (
	"cli/cmd/pod"
	"cli/cmd/pods"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create [command]",
	Short: "create a resource",
	Long:  "create a resource in runpod.io",
}

func init() {
	createCmd.AddCommand(pod.CreatePodCmd)
	createCmd.AddCommand(pods.CreatePodsCmd)
}
