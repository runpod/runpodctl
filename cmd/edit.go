package cmd

import (
	"github.com/runpod/runpodctl/cmd/pod"

	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Use:   "edit [command]",
	Short: "edit resource",
	Long:  "edit config for existing (deployed) resources",
}

func init() {
	editCmd.AddCommand(pod.EditPodCmd)
}
