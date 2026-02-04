package model

import "github.com/spf13/cobra"

// Cmd is the model command group.
var Cmd = &cobra.Command{
	Use:   "model",
	Short: "manage model repository",
	Long:  "manage models in the runpod model repository",
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(addCmd)
	Cmd.AddCommand(removeCmd)
}
