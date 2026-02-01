package pod

import (
	"github.com/spf13/cobra"
)

// Cmd is the pod command group
var Cmd = &cobra.Command{
	Use:     "pod",
	Short:   "manage gpu pods",
	Long:    "manage gpu pods on runpod",
	Aliases: []string{"pods"},
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(getCmd)
	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(updateCmd)
	Cmd.AddCommand(startCmd)
	Cmd.AddCommand(stopCmd)
	Cmd.AddCommand(deleteCmd)
}
