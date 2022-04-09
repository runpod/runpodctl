package pod

import (
	"github.com/spf13/cobra"
)

// PodCmd represents the pod command
var PodCmd = &cobra.Command{
	Use:   "pod",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
}

func init() {
	PodCmd.AddCommand(lsCmd)
}
