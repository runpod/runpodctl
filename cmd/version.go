package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "runpodctl version",
	Long:  "runpodctl version",
	Run: func(c *cobra.Command, args []string) {
		fmt.Println("runpodctl " + version)
	},
}
