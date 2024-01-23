package cmd

import (
	"cli/cmd/pod"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(podCmd)
	podCmd.AddCommand(pod.CreatePodCmd)
	podCmd.AddCommand(pod.StartPodCmd)
	podCmd.AddCommand(pod.StopPodCmd)
	podCmd.AddCommand(pod.RemovePodCmd)
	podCmd.AddCommand(pod.GetPodCmd)
}

var podCmd = &cobra.Command{
	Use:   "pod",
	Short: "pod commands",
	Long:  "pod commands",

	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}
