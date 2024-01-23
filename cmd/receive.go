package cmd

import (
	"cli/cmd/croc"

	"github.com/spf13/cobra"
)

var receiveCmd = &cobra.Command{
	Use:    "receive [code]",
	Args:   cobra.ExactArgs(1),
	Short:  "receive a file",
	Long:   "receive a file",
	Hidden: true,

	Run: func(cmd *cobra.Command, args []string) {
		croc.ReceiveCmd.Run(cmd, args)
	},
}
