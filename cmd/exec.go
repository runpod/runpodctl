package cmd

import (
	"fmt"
	"os"

	"github.com/runpod/runpod/cmd/exec"

	"github.com/spf13/cobra"
)

// execCmd represents the base command for executing commands in a pod
var execCmd = &cobra.Command{
	Use:    "exec",
	Short:  "execute commands in a pod (legacy)",
	Long:   `Execute a local file remotely in a pod.`,
	Hidden: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "warning: 'runpod exec' is deprecated; use 'runpod ssh info <pod-id>' and run your script over SSH")
		fmt.Fprintln(os.Stderr, "note: legacy exec behavior is kept for backward compatibility")
	},
}

func init() {
	execCmd.AddCommand(exec.RemotePythonCmd)
}
