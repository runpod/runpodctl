package cmd

import (
	"cli/cmd/pod"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get [command]",
	Short: "get resource",
	Long:  "get resources for pods",
}

func init() {
	getCmd.AddCommand(pod.GetPodCmd)
}
