package cmd

import (
	"cli/cmd/pod"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create [command]",
	Short: "create a resource",
	Long:  "create a resource in runpod.io",
}

func init() {
	createCmd.AddCommand(pod.CreatePodCmd)
}
