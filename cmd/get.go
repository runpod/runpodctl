package cmd

import (
	"cli/cmd/cloud"
	"cli/cmd/pod"

	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:    "get [command]",
	Short:  "get resource",
	Long:   "get resources for pods",
	Hidden: true,
}

func init() {
	getCmd.AddCommand(cloud.GetCloudCmd)
	getCmd.AddCommand(pod.GetPodCmd)
}
