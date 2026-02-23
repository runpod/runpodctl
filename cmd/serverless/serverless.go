package serverless

import (
	"github.com/spf13/cobra"
)

// Cmd is the serverless command group
var Cmd = &cobra.Command{
	Use:     "serverless",
	Short:   "manage serverless endpoints",
	Long:    "manage serverless endpoints on runpod",
	Aliases: []string{"sls"},
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(getCmd)
	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(updateCmd)
	Cmd.AddCommand(deleteCmd)
}
