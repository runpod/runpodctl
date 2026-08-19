package exec

import (
	"fmt"

	"github.com/spf13/cobra"
)

var RemotePythonCmd = &cobra.Command{
	Use:   "python [file]",
	Short: "deprecated: use ssh instead (still supported)",
	Long:  `deprecated. this command is kept for backward compatibility. use 'runpodctl ssh info <pod-id>' and run your script over ssh.`,
	Args:  cobra.ExactArgs(1),
	// RunE, not Run: a Run handler has no way to report failure, so a failed run
	// used to print to stderr and still exit 0 — `runpodctl exec python x.py &&
	// deploy` deployed anyway. returning the error routes it through the Execute
	// sink, which gives it the flat json shape with a code and exit 1.
	RunE: func(cmd *cobra.Command, args []string) error {
		podID, _ := cmd.Flags().GetString("pod_id")
		pythonCommand, _ := cmd.Flags().GetString("python")
		file := args[0]

		// stays on stdout: legacy commands must preserve their existing stdout
		// exactly (see AGENTS.md), and this is progress, not an error.
		fmt.Println("Running remote Python shell...")
		if err := PythonOverSSH(podID, file, pythonCommand); err != nil {
			return fmt.Errorf("executing python over ssh: %w", err)
		}
		return nil
	},
}

func init() {
	RemotePythonCmd.Flags().String("pod_id", "", "The ID of the pod to run the command on.")
	RemotePythonCmd.Flags().String("python", "python3", "Python interpreter to use (default: python3).")
	RemotePythonCmd.MarkFlagRequired("file")
}
