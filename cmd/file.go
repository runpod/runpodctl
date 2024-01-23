package cmd

import (
	"cli/cmd/croc"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(fileCmd)
	fileCmd.AddCommand(croc.SendCmd)
	fileCmd.AddCommand(croc.ReceiveCmd)
}

var fileCmd = &cobra.Command{
	Use:   "file",
	Short: "file commands",
	Long:  "file commands",

	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}
