package registry

import (
	"github.com/spf13/cobra"
)

// Cmd is the registry command group
var Cmd = &cobra.Command{
	Use:     "registry",
	Short:   "manage container registry auth",
	Long:    "manage container registry authentication on runpod",
	Aliases: []string{"reg"},
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(getCmd)
	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(deleteCmd)
}
