package template

import (
	"github.com/spf13/cobra"
)

// Cmd is the template command group
var Cmd = &cobra.Command{
	Use:     "template",
	Short:   "manage templates",
	Long:    "manage templates on runpod",
	Aliases: []string{"tpl", "templates"},
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(searchCmd)
	Cmd.AddCommand(getCmd)
	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(updateCmd)
	Cmd.AddCommand(deleteCmd)
}
