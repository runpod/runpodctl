package exec

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var RemotePythonCmd = &cobra.Command{
	Use:   "python [file]",
	Short: "deprecated: use ssh instead (still supported)",
	Long:  `deprecated. this command is kept for backward compatibility. use 'runpodctl ssh info <pod-id>' and run your script over ssh.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		podID, _ := cmd.Flags().GetString("pod_id")
		pythonCommand, _ := cmd.Flags().GetString("python")
		file := args[0]

		fmt.Println("Running remote Python shell...")
		if err := PythonOverSSH(podID, file, pythonCommand); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing Python over SSH: %v\n", err)
		}
	},
}

func init() {
	RemotePythonCmd.Flags().String("pod_id", "", "The ID of the pod to run the command on.")
	RemotePythonCmd.Flags().String("python", "python3", "Python interpreter to use (default: python3).")
	RemotePythonCmd.MarkFlagRequired("file")
}
