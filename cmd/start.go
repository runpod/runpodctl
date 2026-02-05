package cmd

import (
	"github.com/runpod/runpodctl/cmd/pod"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start [command]",
	Short: "start a resource",
	Long:  "start a resource in runpod.io",
}

func init() {
	startCmd.AddCommand(pod.StartPodCmd)
}
