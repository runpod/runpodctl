package datacenter

import (
	"github.com/spf13/cobra"
)

// Cmd is the datacenter command group
var Cmd = &cobra.Command{
	Use:     "datacenter",
	Aliases: []string{"dc", "datacenters"},
	Short:   "list datacenters",
	Long:    "list datacenters and their gpu availability",
}

func init() {
	Cmd.AddCommand(listCmd)
}
