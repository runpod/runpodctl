package gpu

import (
	"github.com/spf13/cobra"
)

// Cmd is the gpu command group
var Cmd = &cobra.Command{
	Use:     "gpu",
	Aliases: []string{"gpus"},
	Short:   "list available gpu types",
	Long:    "list available gpu types and their availability",
}

func init() {
	Cmd.AddCommand(listCmd)
}
