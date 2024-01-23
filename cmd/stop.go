package cmd

import (
	"cli/cmd/pod"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:    "stop [command]",
	Short:  "stop a resource",
	Long:   "stop a resource in runpod.io",
	Hidden: true,
}

func init() {
	stopCmd.AddCommand(pod.StopPodCmd)
}
