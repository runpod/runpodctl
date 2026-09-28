package pod

import (
	"fmt"

	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset <pod-id>",
	Short: "reset a pod",
	Long:  "reset a pod (not supported by api v2; use restart to restart it)",
	Args:  cobra.ExactArgs(1),
	RunE:  runReset,
}

func runReset(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("pod reset is not supported by api v2; use 'runpodctl pod restart' to restart a pod")
}
