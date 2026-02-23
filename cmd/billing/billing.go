package billing

import (
	"github.com/spf13/cobra"
)

// Cmd is the billing command group
var Cmd = &cobra.Command{
	Use:   "billing",
	Short: "view billing history",
	Long:  "view billing history for pods, serverless, and network volumes",
}

func init() {
	Cmd.AddCommand(podsCmd)
	Cmd.AddCommand(serverlessCmd)
	Cmd.AddCommand(networkVolumeCmd)
}
