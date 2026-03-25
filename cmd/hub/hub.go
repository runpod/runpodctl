package hub

import (
	"github.com/spf13/cobra"
)

// Cmd is the hub command group
var Cmd = &cobra.Command{
	Use:   "hub",
	Short: "browse the runpod hub",
	Long:  "browse and search the runpod hub for deployable repos",
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(searchCmd)
	Cmd.AddCommand(getCmd)
}
