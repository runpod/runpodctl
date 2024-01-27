package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "runpod cli version",
	Long:  "runpod cli version",
	Run: func(c *cobra.Command, args []string) {
		fmt.Println("runpod " + version)
	},
}
