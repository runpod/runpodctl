package cmd

import (
	"cli/cmd/croc"

	"github.com/spf13/cobra"
)

var sendCmd = &cobra.Command{
	Use:    "send [filename(s) or folder]",
	Args:   cobra.ExactArgs(1),
	Short:  "send a file",
	Long:   "send a file",
	Hidden: true,

	Run: func(cmd *cobra.Command, args []string) {
		croc.SendCmd.Run(cmd, args)
	},
}
