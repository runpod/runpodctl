package exec

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var RemotePythonCmd = &cobra.Command{
	Use:   "python [file]",
	Short: "deprecated: use ssh and run the script manually",
	Long:  `Deprecated. This command no longer runs code. Use 'runpod ssh info <pod-id>' and run your script over SSH.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "warning: 'runpod exec' is deprecated and does not run code")
		fmt.Fprintln(os.Stderr, "use 'runpod ssh info <pod-id>' and run your script over SSH")
	},
}

func init() {
	RemotePythonCmd.Flags().String("pod_id", "", "The ID of the pod to run the command on.")
	RemotePythonCmd.Flags().String("python", "python3", "Python interpreter to use (default: python3).")
	RemotePythonCmd.MarkFlagRequired("file")
}
