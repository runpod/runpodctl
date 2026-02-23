package volume

import (
	"github.com/spf13/cobra"
)

// Cmd is the network-volume command group
var Cmd = &cobra.Command{
	Use:     "network-volume",
	Short:   "manage network volumes",
	Long:    "manage network volumes on runpod",
	Aliases: []string{"nv"},
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(getCmd)
	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(updateCmd)
	Cmd.AddCommand(deleteCmd)
}
