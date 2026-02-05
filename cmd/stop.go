package cmd

import (
	"github.com/runpod/runpodctl/cmd/pod"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop [command]",
	Short: "stop a resource",
	Long:  "stop a resource in runpod.io",
}

func init() {
	stopCmd.AddCommand(pod.StopPodCmd)
}
