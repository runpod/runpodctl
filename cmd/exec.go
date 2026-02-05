package cmd

import (
	"fmt"
	"os"

	"github.com/runpod/runpodctl/cmd/exec"

	"github.com/spf13/cobra"
)

// execCmd represents the base command for executing commands in a pod
var execCmd = &cobra.Command{
	Use:    "exec",
	Short:  "execute commands in a pod (legacy)",
	Long:   `execute a local file remotely in a pod.`,
	Hidden: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "warning: 'runpodctl exec' is deprecated; use 'runpodctl ssh info <pod-id>' and run your script over ssh")
		fmt.Fprintln(os.Stderr, "note: legacy exec behavior is kept for backward compatibility")
	},
}

func init() {
	execCmd.AddCommand(exec.RemotePythonCmd)
}
