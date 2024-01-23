package cmd

import (
	"cli/cmd/pod"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:    "start [command]",
	Short:  "start a resource",
	Long:   "start a resource in runpod.io",
	Hidden: true,
}

func init() {
	startCmd.AddCommand(pod.StartPodCmd)
}
